output "static_ip" {
  description = "External static IP of the VM (the A record points here)."
  value       = google_compute_address.static.address
}

output "url" {
  description = "Public HTTPS URL once DNS resolves and TLS is issued."
  value       = "https://${var.domain}"
}

output "ssh_command" {
  description = "Convenience SSH command via gcloud."
  value       = "gcloud compute ssh ${var.instance_name} --zone ${var.zone} --project ${var.project_id}"
}

output "secret_id" {
  description = "Secret Manager secret holding the .env."
  value       = google_secret_manager_secret.env.secret_id
}

output "service_account_email" {
  description = "Email of the VM's service account."
  value       = google_service_account.vm.email
}
