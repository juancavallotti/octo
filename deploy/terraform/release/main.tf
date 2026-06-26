# The octo Helm release, owned by Terraform. Cloud Build applies this on a version
# tag (_DEPLOY=true); to run it by hand use `task deploy TAG=v0.1.1` (which derives
# chart_version from helm/Chart.yaml), or apply directly:
#
#   terraform -chdir=deploy/terraform/release apply \
#     -var-file=../octo.tfvars -var image_tag=v0.1.1 -var chart_version=0.1.2
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
# Manager); rotating it would log everyone out, so it is kept in state. Only created
# when SSO is enabled.
resource "random_password" "auth_secret" {
  count   = local.oidc_enabled_eff ? 1 : 0
  length  = 32
  special = false
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
# `task deploy` supplies them via octo.tfvars and seeds oidc.json here; the Cloud
# Build deploy step runs without the (gitignored) tfvars, passes no OIDC vars, and
# reads them back from the bucket. The mutually-exclusive counts (write when supplied,
# read otherwise) keep the resource and data source from referencing each other.
locals {
  oidc_provided = var.oidc_client_id != ""
  oidc_stored   = local.oidc_provided ? null : jsondecode(data.google_storage_bucket_object_content.oidc[0].content)

  oidc_enabled_eff       = local.oidc_provided ? var.oidc_enabled : try(local.oidc_stored.enabled, false)
  oidc_issuer_eff        = local.oidc_provided ? var.oidc_issuer : try(local.oidc_stored.issuer, var.oidc_issuer)
  oidc_client_id_eff     = local.oidc_provided ? var.oidc_client_id : try(local.oidc_stored.client_id, "")
  oidc_client_secret_eff = local.oidc_provided ? var.oidc_client_secret : try(local.oidc_stored.client_secret, "")
  oidc_write_roles_eff   = local.oidc_provided ? var.oidc_write_roles : try(local.oidc_stored.write_roles, "")
  oidc_roles_claim_eff   = local.oidc_provided ? var.oidc_roles_claim : try(local.oidc_stored.roles_claim, "")
}

resource "google_storage_bucket_object" "oidc" {
  count  = local.oidc_provided ? 1 : 0
  bucket = local.state_bucket
  name   = "release/oidc.json"
  content = jsonencode({
    enabled       = var.oidc_enabled
    issuer        = var.oidc_issuer
    client_id     = var.oidc_client_id
    client_secret = var.oidc_client_secret
    write_roles   = var.oidc_write_roles
    roles_claim   = var.oidc_roles_claim
  })
}

data "google_storage_bucket_object_content" "oidc" {
  count  = local.oidc_provided ? 0 : 1
  bucket = local.state_bucket
  name   = "release/oidc.json"
}

module "octo" {
  source = "../modules/helm-release"

  namespace         = var.namespace
  image_base        = local.image_base
  chart_version     = var.chart_version
  image_tag         = var.image_tag
  registry_password = data.google_client_config.current.access_token
  domain            = var.domain
  postgres_password = random_password.postgres.result
  cluster_issuer    = var.cluster_issuer
  wildcard_tls      = var.wildcard_tls

  # OIDC SSO. A local deploy supplies client id/secret via octo.tfvars (which also
  # seeds oidc.json in the bucket); Cloud Build reads them back from there. The
  # session secret is generated above. All land in the release state, not Secret Manager.
  oidc_enabled       = local.oidc_enabled_eff
  oidc_issuer        = local.oidc_issuer_eff
  oidc_client_id     = local.oidc_client_id_eff
  oidc_client_secret = local.oidc_client_secret_eff
  auth_secret        = local.oidc_enabled_eff ? random_password.auth_secret[0].result : ""
  oidc_write_roles   = local.oidc_write_roles_eff
  oidc_roles_claim   = local.oidc_roles_claim_eff

  # KV secret-namespace encryption key (base64), generated above and held in state.
  kv_encryption_key = random_bytes.kv_encryption_key.base64

  depends_on = [null_resource.pull_images]
}
