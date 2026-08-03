output "cluster_name" {
  description = "Cluster name, for `gcloud container clusters get-credentials`."
  value       = module.cluster.name
}

output "cluster_location" {
  description = "Cluster location (zone for a zonal Standard cluster, region otherwise)."
  value       = module.cluster.location
}

output "ingress_ip" {
  description = "Reserved external IP the DNS records point at and the ingress controller serves on."
  value       = module.cluster.ingress_ip
}

output "network_name" {
  description = "VPC the cluster lives in. Exposed for teardown: `task gke:*:destroy` sweeps orphaned k8s-* firewall rules off this network before deleting it, since a network with a firewall rule still attached cannot be deleted."
  value       = module.cluster.network_name
}

output "database" {
  description = "Which database the release is running against — the managed instance, or the chart's bundled StatefulSet."
  value       = var.external_database ? "cloudsql:${module.cloudsql[0].instance_name} (${module.cloudsql[0].private_ip})" : "bundled postgres StatefulSet"
}

output "release_status" {
  description = "Helm release status."
  value       = module.octo.release_status
}

# --- Provider wiring ---
# The calling root configures the kubernetes and helm providers from these, so the
# cluster is reached with a short-lived access token and no kubeconfig is ever
# written to disk.

output "cluster_endpoint" {
  description = "Control-plane IP (prefix with https://)."
  value       = module.cluster.endpoint
}

output "cluster_ca_certificate" {
  description = "Base64-encoded cluster CA certificate."
  value       = module.cluster.ca_certificate
  sensitive   = true
}
