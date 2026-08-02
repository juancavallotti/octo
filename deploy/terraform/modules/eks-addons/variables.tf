variable "cluster_name" {
  type        = string
  description = "EKS cluster name. The Load Balancer Controller uses it to find and tag the resources it owns."
}

variable "vpc_id" {
  type        = string
  description = "VPC the cluster runs in. The controller needs it explicitly to discover subnets."
}

variable "oidc_provider_arn" {
  type        = string
  description = "ARN of the cluster's IAM OIDC provider (module.eks.oidc_provider_arn). This is what lets a ServiceAccount assume an IAM role."
}

variable "lb_controller_version" {
  type        = string
  description = "aws-load-balancer-controller chart version. Pinned so a cluster rebuild installs the controller that was tested, not whatever is newest."
  default     = "1.11.0"
}

variable "gp3_is_default" {
  type        = bool
  description = "Mark gp3 as the cluster's default StorageClass. The chart names gp3 explicitly in values-eks.yaml, so this only affects other workloads."
  default     = true
}

variable "timeout" {
  type        = number
  description = "Helm timeout in seconds for the controller install."
  default     = 600
}

variable "tags" {
  type        = map(string)
  description = "Tags applied to the IAM role."
  default     = {}
}
