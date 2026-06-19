output "release_status" {
  description = "Status of the octo Helm release."
  value       = module.octo.release_status
}

output "url" {
  description = "Editor URL once TLS is issued."
  value       = "https://${var.domain}"
}
