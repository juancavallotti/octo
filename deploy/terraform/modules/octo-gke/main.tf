# octo on GKE, in either flavour: the cluster, its prerequisite controllers, DNS
# records, an optional Cloud SQL instance, and the chart itself.
#
# Both GKE roots are this module with different defaults — gke-standard and
# gke-autopilot differ in the `autopilot` flag, which values profile they pass, and
# how much of the node configuration is meaningful. Keeping the composition in one
# place is what stops the two from quietly drifting apart, which matters because
# the whole point of having both is that the SAME deployment is exercised on each.
#
# The kubernetes and helm providers are configured by the calling root (from an
# access token, no kubeconfig on disk) and inherited here.
#
# Two things the caller must know:
#
#   * The DNS zone is NOT managed here. It is created once by hand and delegated
#     from the registrar (see ../../README.md), because Cloud DNS assigns fresh
#     nameservers on every zone creation — a zone inside a root you destroy and
#     recreate would invalidate the delegation every cycle. Records ARE owned here,
#     so they come and go with the cluster.
#   * Both roots write the same record names by default. Destroy one before
#     applying the other.

locals {
  # The repo root, from deploy/terraform/modules/octo-gke.
  repo_root = "${path.module}/../../../.."

  # Values profiles are always read from this checkout: the module reads them with
  # file() at plan time, so they come from the working tree even when the chart
  # itself is pulled from ghcr.
  values_profile = "${local.repo_root}/helm/values-gke-${var.autopilot ? "autopilot" : "standard"}.yaml"

  chart_repository = var.chart_source == "oci" ? "oci://ghcr.io/juancavallotti/charts" : ""
  chart            = var.chart_source == "oci" ? "octo" : "${local.repo_root}/helm"
}

data "google_client_config" "default" {}

# The delegated zone, created by hand and shared with nothing else. Looked up
# rather than managed so `terraform destroy` cannot take the delegation with it.
data "google_dns_managed_zone" "this" {
  name    = var.dns_managed_zone
  project = var.project_id
}

module "cluster" {
  source = "../gke-cluster"

  project_id = var.project_id
  name       = var.name
  region     = var.region
  zone       = var.zone

  autopilot = var.autopilot
  regional  = var.regional

  machine_type      = var.machine_type
  node_count        = var.node_count
  node_min_count    = var.node_min_count
  node_max_count    = var.node_max_count
  spot_nodes        = var.spot_nodes
  private_nodes     = var.private_nodes
  node_disk_size_gb = var.node_disk_size_gb

  deletion_protection        = var.deletion_protection
  master_authorized_networks = var.master_authorized_networks

  # Only the external-database path needs the Cloud SQL peering.
  enable_private_services_access = var.external_database
}

module "addons" {
  source = "../cluster-addons"

  project_id       = var.project_id
  name             = var.name
  load_balancer_ip = module.cluster.ingress_ip
  acme_email       = var.acme_email
  acme_server      = var.acme_server

  # DNS-01 is the only ACME challenge that can issue the *.{apps_domain} wildcard
  # the per-integration ingresses share.
  enable_dns01 = true
}

# --- DNS ---
# Created from the reserved IP, so they resolve before the ingress controller is
# even up and cert-manager's HTTP-01 challenge has somewhere to land on first try.

resource "google_dns_record_set" "editor" {
  name         = "${var.domain}."
  type         = "A"
  ttl          = 60
  managed_zone = data.google_dns_managed_zone.this.name
  project      = var.project_id
  rrdatas      = [module.cluster.ingress_ip]
}

resource "google_dns_record_set" "apps" {
  count        = var.apps_domain != "" && var.apps_domain != var.domain ? 1 : 0
  name         = "${var.apps_domain}."
  type         = "A"
  ttl          = 60
  managed_zone = data.google_dns_managed_zone.this.name
  project      = var.project_id
  rrdatas      = [module.cluster.ingress_ip]
}

# Per-integration endpoints: the orchestrator creates {slug}.{apps_domain} at
# runtime, so the wildcard has to already resolve.
resource "google_dns_record_set" "wildcard" {
  count        = var.apps_domain != "" ? 1 : 0
  name         = "*.${var.apps_domain}."
  type         = "A"
  ttl          = 60
  managed_zone = data.google_dns_managed_zone.this.name
  project      = var.project_id
  rrdatas      = [module.cluster.ingress_ip]
}

# --- Database ---

module "cloudsql" {
  source = "../cloudsql"
  count  = var.external_database ? 1 : 0

  project_id = var.project_id
  name       = "${var.name}-pg"
  region     = var.region
  network_id = module.cluster.network_id
  tier       = var.cloudsql_tier

  depends_on = [module.cluster]
}

# The bundled Postgres path generates its own password instead.
resource "random_password" "postgres" {
  count   = var.external_database ? 0 : 1
  length  = 24
  special = false
}

resource "kubernetes_namespace_v1" "octo" {
  metadata {
    name = var.namespace
  }
  # Nothing can be created in a namespace on a cluster that is not up yet, and the
  # addons' webhooks must be serving before the chart's resources are admitted.
  depends_on = [module.addons]
}

# The managed database's password reaches the chart through a Secret, never through
# a Helm value: Helm stores every value it is given in the release history Secret,
# where it outlives any rotation. This is also the path helm/values-gke-*.yaml and
# the docs tell users to take, so it is the one worth exercising.
resource "kubernetes_secret_v1" "db" {
  count = var.external_database ? 1 : 0

  metadata {
    name      = "octo-cloudsql"
    namespace = kubernetes_namespace_v1.octo.metadata[0].name
  }

  data = {
    password = module.cloudsql[0].password
  }
}

# --- Application secrets ---
#
# Minted here rather than asked for. Neither has anything to stay consistent with
# across a destroy/recreate cycle — the database holding the sessions and the
# ciphertext goes with the cluster — and both are stable across applies because
# they live in this root's state.

# Auth.js session secret. Created unconditionally so that turning SSO on later is
# an in-place update rather than a new secret; only passed to the chart when SSO
# is actually enabled, which is the only case where the editor reads it.
resource "random_password" "auth_secret" {
  length  = 32
  special = false
}

# KV secret-namespace encryption key: 32 random bytes (AES-256), base64 for the
# chart. Not optional in practice — with kv.encryptionKey empty the chart rejects
# every write to a secret KV namespace (plain KV keeps working), so an integration
# that stores a credential fails on this cluster and nowhere else. That is a
# property of the deployment, not of the chart under test, so it is not something
# to leave to a tfvars line someone forgets.
#
# ignore_changes on length: regenerating it would orphan existing ciphertext.
resource "random_bytes" "kv_encryption_key" {
  length = 32

  lifecycle {
    ignore_changes = [length]
  }
}

# --- The release ---

module "octo" {
  source = "../helm-release"

  release_name     = "octo"
  namespace        = kubernetes_namespace_v1.octo.metadata[0].name
  create_namespace = false

  repository    = local.chart_repository
  chart         = local.chart
  chart_version = var.chart_version

  # Left empty so the chart's own defaults apply: the images are public on Docker
  # Hub at the chart's appVersion. image_values_file overrides both when testing
  # against a working-tree build — see `task images:ttl`.
  image_registry    = ""
  image_tag         = ""
  image_pull_policy = ""
  image_values_file = var.image_values_file

  values_files = [local.values_profile]

  domain      = var.domain
  apps_domain = var.apps_domain != "" ? var.apps_domain : var.domain

  cluster_issuer          = module.addons.cluster_issuer_name
  wildcard_tls            = var.apps_domain != ""
  wildcard_cluster_issuer = module.addons.dns01_cluster_issuer_name

  postgres_password = var.external_database ? "" : random_password.postgres[0].result
  external_database = var.external_database ? {
    host            = module.cloudsql[0].private_ip
    user            = module.cloudsql[0].database_user
    database        = module.cloudsql[0].database_name
    sslmode         = "require"
    existing_secret = kubernetes_secret_v1.db[0].metadata[0].name
  } : null

  # --- Editor authentication ---
  oidc_enabled       = var.oidc_enabled
  oidc_issuer        = var.oidc_issuer
  oidc_client_id     = var.oidc_client_id
  oidc_client_secret = var.oidc_client_secret
  oidc_write_roles   = var.oidc_write_roles
  oidc_roles_claim   = var.oidc_roles_claim
  auth_secret        = var.oidc_enabled ? random_password.auth_secret.result : ""

  kv_encryption_key = random_bytes.kv_encryption_key.base64

  # Roll back a failed install rather than leaving half a release to inspect and
  # clean up by hand — this environment is rebuilt often enough that a clean
  # failure is worth more than a forensic one.
  atomic  = true
  timeout = var.helm_timeout

  # The ClusterIssuers must exist before the chart's Certificates reference them,
  # and the ingress controller before its Ingress means anything.
  depends_on = [module.addons]
}
