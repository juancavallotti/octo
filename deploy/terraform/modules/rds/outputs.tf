output "endpoint" {
  description = "host:port. Split before use — the chart takes host and port separately."
  value       = aws_db_instance.this.endpoint
}

output "host" {
  description = "Hostname the chart's externalDatabase.host points at."
  value       = aws_db_instance.this.address
}

output "port" {
  description = "Port the instance listens on."
  value       = aws_db_instance.this.port
}

output "database_name" {
  description = "Database the chart connects to."
  value       = aws_db_instance.this.db_name
}

output "database_user" {
  description = "Master user, and the owner of database_name."
  value       = aws_db_instance.this.username
}

output "password" {
  description = "Generated password. The caller puts this in a Kubernetes Secret and points the chart at it with externalDatabase.existingSecret — it must never be passed as a Helm value, because Helm keeps every value in release history."
  value       = random_password.octo.result
  sensitive   = true
}

output "security_group_id" {
  description = "Security group guarding the instance."
  value       = aws_security_group.this.id
}
