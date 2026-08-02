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

output "service_account_email" {
  description = "Email of the VM's service account."
  value       = google_service_account.vm.email
}

output "data_disk_name" {
  description = "Name of the Postgres data disk. It is not deleted with the instance; deleting it is an explicit act."
  value       = google_compute_disk.data.name
}

output "data_disk_device_name" {
  description = "Device name the data disk is attached under (the guest sees /dev/disk/by-id/google-{this})."
  value       = var.data_disk_device_name
}
