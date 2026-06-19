output "trigger_id" {
  description = "ID of the Cloud Build trigger."
  value       = module.cloudbuild.trigger_id
}

output "image_base" {
  description = "Base path the build pushes to, e.g. us-west1-docker.pkg.dev/PROJECT/octo."
  value       = module.cloudbuild.image_base
}
