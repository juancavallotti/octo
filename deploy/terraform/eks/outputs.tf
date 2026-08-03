output "cluster_name" {
  description = "EKS cluster name."
  value       = module.eks.cluster_name
}

output "kubeconfig_command" {
  description = "Fetch cluster credentials for kubectl. Terraform itself needs none of this — it mints a token per apply — but you will want it to look around."
  value       = "aws eks update-kubeconfig --name ${module.eks.cluster_name} --region ${var.region}"
}

output "alb_hostname" {
  description = "DNS name of the ALB the Load Balancer Controller created, which the Route53 records point at. Empty if it was not yet provisioned when the Ingress was read — raise alb_wait and re-apply."
  value       = local.alb_hostname
}

output "certificate_arn" {
  description = "ACM certificate the ALB terminates TLS with."
  value       = aws_acm_certificate_validation.octo.certificate_arn
}

output "url" {
  description = "Editor URL, once DNS propagates."
  value       = "https://${var.domain}"
}

output "database" {
  description = "Which database the release is running against."
  value       = var.external_database ? "rds:${module.rds[0].host}" : "bundled postgres StatefulSet"
}

output "release_status" {
  description = "Helm release status."
  value       = module.octo.release_status
}
