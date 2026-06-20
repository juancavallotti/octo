# Combined one-time infrastructure for octo: the Artifact Registry, the single-node
# k3s VM (+ cluster bootstrap), and (optionally) the Cloud Build trigger — all in one
# root with one state, applied once. The octo chart itself is installed/upgraded
# separately by deploy/terraform/release (the Terraform Helm provider), run on a tag
# by Cloud Build or manually via `task deploy`.
#
#   cd deploy/terraform/infra
#   terraform init && terraform apply -var-file=../octo.tfvars
#
# Connect the GitHub repo to Cloud Build (console, one-time) before setting
# enable_cloudbuild=true.

locals {
  registry_host = "${var.region}-docker.pkg.dev"
  image_base    = "${local.registry_host}/${var.project_id}/${module.registry.repository_id}"
  secret_id     = "${var.instance_name}-postgres"
  state_bucket  = coalesce(var.state_bucket, "octo-tfstate-${var.project_id}")
  # API reachable by the release root; default to the SSH ranges so the kube API is
  # no more exposed than SSH already is.
  kube_api_source_ranges = coalesce(var.kube_api_source_ranges, var.ssh_source_ranges)
}

# --- Artifact Registry (images + OCI chart) ---
module "registry" {
  source = "../modules/registry"

  project_id    = var.project_id
  region        = var.region
  repository_id = var.repository_id
}

# --- k3s VM + cluster bootstrap ---

# Postgres password: generated once, kept in state, stored in Secret Manager so the
# release root can read it (data source) and pass it to the chart. Alphanumeric only
# so it is safe inside the DATABASE_URL.
resource "random_password" "postgres" {
  length  = 24
  special = false
}

module "base" {
  source = "../modules/base"

  project_id             = var.project_id
  region                 = var.region
  zone                   = var.zone
  machine_type           = var.machine_type
  instance_name          = var.instance_name
  domain                 = var.domain
  dns_managed_zone       = var.dns_managed_zone
  ssh_source_ranges      = var.ssh_source_ranges
  kube_api_source_ranges = local.kube_api_source_ranges
  boot_disk_size_gb      = var.boot_disk_size_gb

  # Traefik (k3s built-in) serves 80/443; 6443 is opened separately by the module.
  web_tcp_ports = ["80", "443"]

  secret_id   = local.secret_id
  secret_data = random_password.postgres.result

  startup_script = templatefile("${path.module}/startup.sh.tftpl", {
    registry_host = local.registry_host
    domain        = var.domain
    acme_email    = var.acme_email
    project_id    = var.project_id
  })

  # octo-pull (image pulls with a fresh token) is delivered via metadata and installed
  # by the startup script; the release root invokes it over SSH.
  metadata = {
    "octo-pull-sh" = templatefile("${path.module}/octo-pull.sh.tftpl", {
      image_base = local.image_base
    })
  }
}

# Let the VM's service account pull images and the chart from Artifact Registry.
# Referencing module.registry.repository_id orders this after the repo exists.
resource "google_artifact_registry_repository_iam_member" "vm_reader" {
  project    = var.project_id
  location   = var.region
  repository = module.registry.repository_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${module.base.service_account_email}"
}

# --- Cloud Build trigger (optional; needs the GitHub App connected first) ---
module "cloudbuild" {
  source = "../modules/cloudbuild"
  count  = var.enable_cloudbuild ? 1 : 0

  project_id    = var.project_id
  region        = var.region
  repository_id = module.registry.repository_id
  github_owner  = var.github_owner
  github_repo   = var.github_repo

  # Let the build roll the cluster after publishing (grants the deploy IAM + sets
  # the _DEPLOY substitution).
  enable_deploy            = var.cloudbuild_auto_deploy
  instance_name            = var.instance_name
  zone                     = var.zone
  domain                   = var.domain
  # Reference the base module's output (not local.secret_id) so the deploy IAM grant
  # is ordered after the secret is created.
  deploy_secret_id         = module.base.secret_id
  state_bucket             = local.state_bucket
  vm_service_account_email = module.base.service_account_email
}
