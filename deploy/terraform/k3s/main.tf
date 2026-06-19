# Single-node k3s VM for octo. This root provisions the infrastructure and
# bootstraps the cluster (k3s + cert-manager + the Let's Encrypt ClusterIssuer);
# the octo chart itself is installed/upgraded by deploy/terraform/release using
# the Terraform Helm provider, applied after images are published.
#
# Prerequisite: deploy/terraform/registry has been applied.

locals {
  secret_id     = "${var.instance_name}-postgres"
  registry_host = "${var.region}-docker.pkg.dev"
  image_base    = "${local.registry_host}/${var.project_id}/${var.repository_id}"
  # API reachable by the operator running the release root; default to the SSH
  # ranges so the kube API is no more exposed than SSH already is.
  kube_api_source_ranges = coalesce(var.kube_api_source_ranges, var.ssh_source_ranges)
}

# Postgres password: generated once, kept in state, stored in Secret Manager so
# the release root can read it (data source) and pass it to the chart. Alphanumeric
# only so it is safe inside the DATABASE_URL.
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

  # Traefik (k3s built-in) serves 80/443; 6443 is opened separately above.
  web_tcp_ports = ["80", "443"]

  secret_id   = local.secret_id
  secret_data = random_password.postgres.result

  startup_script = templatefile("${path.module}/startup.sh.tftpl", {
    registry_host = local.registry_host
    domain        = var.domain
    acme_email    = var.acme_email
  })

  # octo-pull (image pulls with a fresh token) is delivered via metadata and
  # installed by the startup script; the release root invokes it over SSH.
  metadata = {
    "octo-pull-sh" = templatefile("${path.module}/octo-pull.sh.tftpl", {
      image_base = local.image_base
    })
  }
}

# Let the VM's service account pull images and the chart from Artifact Registry.
resource "google_artifact_registry_repository_iam_member" "vm_reader" {
  project    = var.project_id
  location   = var.region
  repository = var.repository_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${module.base.service_account_email}"
}
