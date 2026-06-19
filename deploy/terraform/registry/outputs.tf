output "repository_id" {
  description = "Artifact Registry repository id."
  value       = google_artifact_registry_repository.octo.repository_id
}

output "image_base" {
  description = "Base path for image refs and the OCI chart, e.g. us-central1-docker.pkg.dev/PROJECT/octo. Use as image.registry in the chart and the helm/images push targets."
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.octo.repository_id}"
}
