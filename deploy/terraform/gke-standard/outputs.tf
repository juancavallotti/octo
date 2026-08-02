output "cluster_name" {
  description = "Cluster name, for `gcloud container clusters get-credentials`."
  value       = module.octo_gke.cluster_name
}

output "cluster_location" {
  description = "Cluster location (zone, or region when regional = true)."
  value       = module.octo_gke.cluster_location
}

output "kubeconfig_command" {
  description = "Fetch cluster credentials for kubectl. Terraform itself needs none of this — it authenticates with an access token — but you will want it to look around."
  value       = "gcloud container clusters get-credentials ${module.octo_gke.cluster_name} --location ${module.octo_gke.cluster_location} --project ${var.project_id}"
}

output "ingress_ip" {
  description = "Reserved external IP the DNS records point at and the ingress controller serves on."
  value       = module.octo_gke.ingress_ip
}

output "url" {
  description = "Editor URL, once DNS resolves and cert-manager has issued the certificate."
  value       = "https://${var.domain}"
}

output "database" {
  description = "Which database the release is running against."
  value       = module.octo_gke.database
}

output "release_status" {
  description = "Helm release status."
  value       = module.octo_gke.release_status
}
