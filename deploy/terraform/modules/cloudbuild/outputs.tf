output "trigger_id" {
  description = "ID of the Cloud Build trigger."
  value       = google_cloudbuild_trigger.publish.id
}

output "image_base" {
  description = "Base path the build pushes to, e.g. us-west1-docker.pkg.dev/PROJECT/octo."
  value       = local.image_base
}
