output "lb_controller_role_arn" {
  description = "IAM role the AWS Load Balancer Controller assumes."
  value       = module.lb_controller_irsa.iam_role_arn
}

output "storage_class" {
  description = "Name of the gp3 StorageClass, matching what helm/values-eks.yaml asks for."
  value       = kubernetes_storage_class_v1.gp3.metadata[0].name
}

output "ready" {
  description = "Depend on this to order work after the controller is running. The octo release must not create its Ingress before something exists to reconcile it — and, on destroy, the controller must outlive the Ingress so it can clean up the ALB."
  value       = helm_release.aws_lb_controller.id
}
