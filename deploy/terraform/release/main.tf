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
}

# Auth.js session secret for the editor. Generated here too (state, not Secret
# Manager); rotating it would log everyone out, so it is kept in state. Only created
# when SSO is enabled.
resource "random_password" "auth_secret" {
  count   = var.oidc_enabled ? 1 : 0
  length  = 32
  special = false
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
    # the existing firewall.
    command = "gcloud compute ssh ${var.instance_name} --zone ${var.zone} --project ${var.project_id} --tunnel-through-iap --quiet --command 'sudo octo-pull ${var.image_tag}'"
  }
}

module "octo" {
  source = "../modules/helm-release"

  image_base        = local.image_base
  chart_version     = var.chart_version
  image_tag         = var.image_tag
  registry_password = data.google_client_config.current.access_token
  domain            = var.domain
  postgres_password = random_password.postgres.result
  cluster_issuer    = var.cluster_issuer
  wildcard_tls      = var.wildcard_tls

  # OIDC SSO. client id/secret come from octo.tfvars; the session secret is
  # generated above. All land in the release state (bucket), not Secret Manager.
  oidc_enabled       = var.oidc_enabled
  oidc_issuer        = var.oidc_issuer
  oidc_client_id     = var.oidc_client_id
  oidc_client_secret = var.oidc_client_secret
  auth_secret        = var.oidc_enabled ? random_password.auth_secret[0].result : ""
  oidc_write_roles   = var.oidc_write_roles
  oidc_roles_claim   = var.oidc_roles_claim

  depends_on = [null_resource.pull_images]
}
