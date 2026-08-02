output "image_base" {
  description = "Artifact Registry base for image refs and the OCI chart, e.g. us-west1-docker.pkg.dev/PROJECT/octo."
  value       = local.image_base
}

output "static_ip" {
  description = "External static IP of the VM (the A record points here)."
  value       = module.base.static_ip
}

output "url" {
  description = "Public HTTPS URL of the editor once DNS resolves and cert-manager issues the cert. May differ from apps_domain (module.base.url), which is what DNS is actually managed for."
  value       = "https://${var.domain}"
}

output "ssh_command" {
  description = "Convenience SSH command via gcloud."
  value       = module.base.ssh_command
}

output "kube_api_endpoint" {
  description = "k3s API endpoint the Terraform Helm release connects to."
  value       = "https://${var.domain}:6443"
}

output "service_account_email" {
  description = "Email of the VM's service account."
  value       = module.base.service_account_email
}

output "data_disk_name" {
  description = "Name of the Postgres data disk. Attached to the VM but not owned by it — it survives the instance being replaced."
  value       = module.base.data_disk_name
}

output "postgres_host_path" {
  description = "Path on the data disk that Postgres is pinned to (the release root's postgres_host_path default). Changing it on a release that already holds data is a data move, not a config change — see docs/deployment.md."
  value       = "${var.data_disk_mount_path}/postgres"
}

output "cloudbuild_trigger_id" {
  description = "ID of the Cloud Build trigger (null when enable_cloudbuild = false)."
  value       = var.enable_cloudbuild ? module.cloudbuild[0].trigger_id : null
}
