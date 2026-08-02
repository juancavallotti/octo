output "cluster_issuer_name" {
  description = "Name of the HTTP-01 ClusterIssuer, for the chart's ingress.tls.clusterIssuer."
  value       = var.cluster_issuer_name
}

output "dns01_cluster_issuer_name" {
  description = "Name of the DNS-01 ClusterIssuer, for the chart's wildcardTLS.clusterIssuer. Null when enable_dns01 is off."
  value       = var.enable_dns01 ? var.dns01_cluster_issuer_name : null
}

output "cert_manager_service_account" {
  description = "Email of the Google service account cert-manager's DNS-01 solver impersonates, or null when disabled."
  value       = var.enable_dns01 ? google_service_account.cert_manager[0].email : null
}

output "ready" {
  description = "Depend on this to order work after every addon is installed and its webhooks are serving. The octo release must not install before the ClusterIssuers exist, or its Certificates reference an issuer that is not there yet."
  value       = helm_release.cluster_issuers.id
}
