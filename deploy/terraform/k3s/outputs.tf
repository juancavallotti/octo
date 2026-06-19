output "static_ip" {
  description = "External static IP of the VM (the A record points here)."
  value       = module.base.static_ip
}

output "url" {
  description = "Public HTTPS URL once DNS resolves and cert-manager issues the cert."
  value       = module.base.url
}

output "ssh_command" {
  description = "Convenience SSH command via gcloud."
  value       = module.base.ssh_command
}

output "kube_api_endpoint" {
  description = "k3s API endpoint the Terraform Helm release connects to."
  value       = "https://${var.domain}:6443"
}

output "secret_id" {
  description = "Secret Manager secret holding the generated Postgres password."
  value       = module.base.secret_id
}

output "image_base" {
  description = "Artifact Registry base the VM pulls from."
  value       = local.image_base
}
