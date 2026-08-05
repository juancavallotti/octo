# The octo Helm release, owned by Terraform. Cloud Build applies this on a version
# tag (_DEPLOY=true); to run it by hand use `task deploy TAG=v0.1.1` (which derives
# chart_version from helm/Chart.yaml), or apply directly:
#
#   terraform -chdir=deploy/terraform/release apply \
#     -var image_tag=v0.1.1 -var chart_version=0.1.2
#
# It pulls the target tag onto the node (fresh token, via octo-pull over SSH),
# then installs/upgrades the chart through the Helm provider. A changed image_tag
# rolls the Deployments. Prereqs: deploy/terraform/infra applied, and
# `task deploy:kubeconfig` has produced the kubeconfig.

locals {
  image_base = "${var.region}-docker.pkg.dev/${var.project_id}/${var.repository_id}"
  kubeconfig = var.kubeconfig != "" ? var.kubeconfig : "${path.module}/../infra/kubeconfig.yaml"
  # The release state bucket (matches the literal in backend.tf). Also holds oidc.json
  # so the Cloud Build deploy step can read the OIDC creds without the local tfvars.
  state_bucket = "octo-tfstate-${var.project_id}"
  # The domain per-integration subdomains + the wildcard cert actually use. Falls
  # back to `domain` for the original single-domain setup.
  apps_domain_eff = var.apps_domain != "" ? var.apps_domain : var.domain
}

# Operator access token for pulling the OCI chart from Artifact Registry.
data "google_client_config" "current" {}

# Postgres password. Generated here and held in the release state (the GCS bucket) —
# no Secret Manager. Kept in state so it stays stable across applies (alphanumeric so
# it is safe inside the DATABASE_URL). The chart writes it into the cluster Secret and
# the StatefulSet on first init.
resource "random_password" "postgres" {
  length  = 24
  special = false

  # `terraform import` can't recover the original generation flags, so an adopted
  # password comes back with special=true and would otherwise force a regenerate
  # (rotating the live DB password). Ignore the generation flags: the stored value
  # is what matters, and fresh clusters still create an alphanumeric password.
  lifecycle {
    ignore_changes = [special, length, min_special, override_special]
  }
}

# Auth.js session secret for the editor. Generated here too (state, not Secret
# Manager); rotating it would log everyone out, so it is kept in state. Always
# created (not gated on SSO): a Cloud Build deploy has no terraform.tfvars, so gating on
# oidc_enabled_eff would let count drop to 0 and destroy the secret — the chart then
# comes up without AUTH_SECRET and Auth.js throws MissingSecret. Keeping it
# unconditional pins the value in the shared release state across every deploy; it is
# only passed to the chart when SSO is enabled (see auth_secret below).
resource "random_password" "auth_secret" {
  length  = 32
  special = false
}

# The secret used to be count-gated (auth_secret[0]); preserve the existing value on
# upgrade instead of destroy+recreate (which would log everyone out).
moved {
  from = random_password.auth_secret[0]
  to   = random_password.auth_secret
}

# KV secret-namespace encryption key: 32 random bytes (AES-256), base64-encoded for
# the chart. Held in the release state and stable across applies — rotating it would
# orphan existing secret ciphertext, so the generation input is ignored (as with the
# postgres password).
resource "random_bytes" "kv_encryption_key" {
  length = 32

  lifecycle {
    ignore_changes = [length]
  }
}

# Pull the target tag onto the node with a fresh token so the chart's pods
# (imagePullPolicy IfNotPresent) find the images locally. Re-runs when the tag
# changes; the chart install/upgrade depends on it.
resource "null_resource" "pull_images" {
  triggers = {
    image_tag = var.image_tag
  }

  provisioner "local-exec" {
    interpreter = ["bash", "-c"]
    # --tunnel-through-iap so this works the same from a laptop and from the Cloud
    # Build deploy step (whose egress IP is dynamic); the IAP range reaches SSH via
    # the existing firewall. SSH as a NON-root user (octodeploy): the Cloud Build
    # gcloud container runs as root, and GCE rejects metadata-key root SSH
    # ("Permission denied (publickey)") because the guest agent only provisions
    # keys for non-root users (with passwordless sudo). Retried because the first
    # SSH after the key is pushed fails until it propagates to the instance.
    command = "for i in $(seq 1 5); do gcloud compute ssh octodeploy@${var.instance_name} --zone ${var.zone} --project ${var.project_id} --tunnel-through-iap --quiet --command 'sudo octo-pull ${var.image_tag}' && exit 0; echo \"octo-pull SSH attempt $i failed; retrying in 15s\" >&2; sleep 15; done; exit 1"
  }
}

# OIDC creds, persisted in the release state bucket (no Secret Manager). A local
# `task deploy` supplies them via terraform.tfvars and seeds oidc.json here; the Cloud
# Build deploy step runs without the (gitignored) tfvars, passes no OIDC vars, and
# reads them back from the bucket. The mutually-exclusive counts (write when supplied,
# read otherwise) keep the resource and data source from referencing each other.
#
# The read is also gated on the object existing: on a fresh environment (or one
# that never configured OIDC) oidc.json is absent, and reading it directly would
# 404 the whole apply. Listing the bucket first lets a missing object mean simply
# "OIDC disabled" instead of a hard failure.
data "google_storage_bucket_objects" "state" {
  bucket = local.state_bucket
  prefix = "release/"
}

locals {
  oidc_provided = var.oidc_client_id != ""
  oidc_exists = contains(
    [for o in data.google_storage_bucket_objects.state.bucket_objects : o.name],
    "release/oidc.json",
  )
  # Read stored OIDC only when it was not supplied locally and the object exists.
  oidc_read   = !local.oidc_provided && local.oidc_exists
  oidc_stored = local.oidc_read ? jsondecode(data.google_storage_bucket_object_content.oidc[0].content) : null

  oidc_enabled_eff       = local.oidc_provided ? var.oidc_enabled : try(local.oidc_stored.enabled, false)
  oidc_issuer_eff        = local.oidc_provided ? var.oidc_issuer : try(local.oidc_stored.issuer, var.oidc_issuer)
  oidc_client_id_eff     = local.oidc_provided ? var.oidc_client_id : try(local.oidc_stored.client_id, "")
  oidc_client_secret_eff = local.oidc_provided ? var.oidc_client_secret : try(local.oidc_stored.client_secret, "")
  oidc_write_roles_eff   = local.oidc_provided ? var.oidc_write_roles : try(local.oidc_stored.write_roles, "")
  oidc_roles_claim_eff   = local.oidc_provided ? var.oidc_roles_claim : try(local.oidc_stored.roles_claim, "")
}

# Persisted OIDC config so a Cloud Build deploy (which has no terraform.tfvars) can read
# the creds back. Gated on oidc_enabled_eff — NOT oidc_provided — and written from
# the effective locals, so a CI apply that resolved these from this very file keeps
# count=1 and rewrites identical content (idempotent) instead of destroying the seed.
# It is only removed when SSO is genuinely turned off (oidc_enabled_eff = false).
resource "google_storage_bucket_object" "oidc" {
  count  = local.oidc_enabled_eff ? 1 : 0
  bucket = local.state_bucket
  name   = "release/oidc.json"
  content = jsonencode({
    enabled       = local.oidc_enabled_eff
    issuer        = local.oidc_issuer_eff
    client_id     = local.oidc_client_id_eff
    client_secret = local.oidc_client_secret_eff
    write_roles   = local.oidc_write_roles_eff
    roles_claim   = local.oidc_roles_claim_eff
  })
}

data "google_storage_bucket_object_content" "oidc" {
  count  = local.oidc_read ? 1 : 0
  bucket = local.state_bucket
  name   = "release/oidc.json"
}

module "octo" {
  source = "../modules/helm-release"

  namespace = var.namespace

  # Chart and images both come out of the same Artifact Registry repo, but they are
  # two distinct settings in the module: the OCI chart repo (with a GCP access token)
  # and the registry the chart writes into every image reference.
  repository        = "oci://${local.image_base}"
  chart             = "octo"
  chart_version     = var.chart_version
  registry_username = "oauth2accesstoken"
  registry_password = data.google_client_config.current.access_token

  image_registry     = local.image_base
  image_tag          = var.image_tag
  image_values_file  = var.image_values_file
  values_files       = var.values_files
  domain             = var.domain
  apps_domain        = local.apps_domain_eff
  postgres_password  = random_password.postgres.result
  postgres_host_path = var.postgres_host_path
  # The path is a mount point on the attached data disk, so kubelet must not
  # invent it. If the disk failed to mount, the chart's DirectoryOrCreate default
  # would put the database on the boot disk and nothing would say so until the
  # next VM rebuild took it. Directory turns that into a Pending pod naming the
  # missing path — see modules/base and infra/startup.sh.tftpl.
  postgres_host_path_type = "Directory"
  ingress_class           = var.ingress_class
  cluster_issuer          = var.cluster_issuer
  wildcard_tls            = var.wildcard_tls

  # OIDC SSO. A local deploy supplies client id/secret via terraform.tfvars (which also
  # seeds oidc.json in the bucket); Cloud Build reads them back from there. The
  # session secret is generated above. All land in the release state, not Secret Manager.
  oidc_enabled       = local.oidc_enabled_eff
  oidc_issuer        = local.oidc_issuer_eff
  oidc_client_id     = local.oidc_client_id_eff
  oidc_client_secret = local.oidc_client_secret_eff
  auth_secret        = local.oidc_enabled_eff ? random_password.auth_secret.result : ""
  oidc_write_roles   = local.oidc_write_roles_eff
  oidc_roles_claim   = local.oidc_roles_claim_eff

  # KV secret-namespace encryption key (base64), generated above and held in state.
  kv_encryption_key = random_bytes.kv_encryption_key.base64

  depends_on = [null_resource.pull_images]
}
