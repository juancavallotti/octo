# Artifact Registry that hosts octo's container images and the OCI Helm chart.
# `task images:push` / `task helm:push` (and the Cloud Build trigger) publish into it;
# the VM service account is granted read access by the caller (see the infra root).

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
