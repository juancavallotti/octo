output "instance_name" {
  description = "Cloud SQL instance name."
  value       = google_sql_database_instance.this.name
}

output "connection_name" {
  description = "PROJECT:REGION:INSTANCE, for the Cloud SQL Auth Proxy or `gcloud sql connect`."
  value       = google_sql_database_instance.this.connection_name
}

output "private_ip" {
  description = "Private IP the chart's externalDatabase.host points at. Reachable from pods because the cluster is VPC-native."
  value       = google_sql_database_instance.this.private_ip_address
}

output "database_name" {
  description = "Database the chart connects to."
  value       = google_sql_database.octo.name
}

output "database_user" {
  description = "Database user, and the owner of database_name."
  value       = google_sql_user.octo.name
}

output "password" {
  description = "Generated password. The caller puts this in a Kubernetes Secret and points the chart at it with externalDatabase.existingSecret — it must never be passed as a Helm value, because Helm keeps every value in release history."
  value       = random_password.octo.result
  sensitive   = true
}
