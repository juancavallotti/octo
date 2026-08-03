output "name" {
  description = "Cluster name."
  value       = google_container_cluster.this.name
}

output "location" {
  description = "Cluster location (region for Autopilot/regional, zone for a zonal Standard cluster). Needed by `gcloud container clusters get-credentials`."
  value       = google_container_cluster.this.location
}

output "endpoint" {
  description = "Control-plane IP. Prefix with https:// for the kubernetes/helm providers."
  value       = google_container_cluster.this.endpoint
}

output "ca_certificate" {
  description = "Base64-encoded cluster CA certificate for the kubernetes/helm providers."
  value       = google_container_cluster.this.master_auth[0].cluster_ca_certificate
  sensitive   = true
}

output "ingress_ip" {
  description = "Reserved external IP for the ingress controller's LoadBalancer Service. Known before the controller exists, which is what lets the DNS records be created in the same apply."
  value       = google_compute_address.ingress.address
}

output "network_id" {
  description = "Self-link of the VPC (what a Cloud SQL private-IP instance attaches to)."
  value       = google_compute_network.this.id
}

output "network_name" {
  description = "Name of the VPC."
  value       = google_compute_network.this.name
}

output "subnetwork_id" {
  description = "Self-link of the subnet."
  value       = google_compute_subnetwork.this.id
}

output "node_pool_id" {
  description = "ID of the Standard node pool, or null on Autopilot. Depend on this to order work after nodes exist."
  value       = var.autopilot ? null : google_container_node_pool.primary[0].id
}

output "private_services_connection" {
  description = "ID of the service-networking connection, or null when disabled. Depend on this so a Cloud SQL instance is not created before the peering it needs."
  value       = var.enable_private_services_access ? google_service_networking_connection.psa[0].id : null
}
