# Artifact Registry that hosts octo's container images and the OCI Helm chart.
# Applied once (independently of the k3s deployment); `task images:push` and
# `task helm:push` publish into it, and the k3s variant grants the VM service
# account read access (see deploy/terraform/k3s).

resource "google_project_service" "artifactregistry" {
  service            = "artifactregistry.googleapis.com"
  disable_on_destroy = false
}

resource "google_artifact_registry_repository" "octo" {
  location      = var.region
  repository_id = var.repository_id
  description   = "octo images and Helm chart"
  format        = "DOCKER"

  depends_on = [google_project_service.artifactregistry]
}
