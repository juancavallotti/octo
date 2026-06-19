output "release_name" {
  description = "Name of the Helm release."
  value       = helm_release.octo.name
}

output "release_status" {
  description = "Status of the Helm release."
  value       = helm_release.octo.status
}
