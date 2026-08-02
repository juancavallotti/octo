# Variables for the `eks` root. Values come from terraform.tfvars, which Terraform
# loads automatically — see terraform.tfvars.example.

variable "region" {
  type        = string
  description = "AWS region for everything in this root."
  default     = "us-east-1"
}

variable "name" {
  type        = string
  description = "Prefix for every AWS resource: the VPC, cluster, node group, IAM roles and the RDS instance."
  default     = "octo-eks"
}

# --- Hostnames + DNS ---

variable "route53_zone_name" {
  type        = string
  description = "Name of the Route53 hosted zone the records go in. Created by hand once and delegated from whoever holds the parent domain — this root creates records in it but never the zone itself, so a destroy cannot take the delegation with it."
  default     = "aws.octopaas.dev"
}

variable "domain" {
  type        = string
  description = "Editor hostname. Must be a subdomain, not the zone apex: the record pointing at the ALB is a CNAME."
  default     = "octo.aws.octopaas.dev"
}

variable "apps_domain" {
  type        = string
  description = "Base hostname per-integration endpoints live under ({slug}.{apps_domain}). Gets a wildcard CNAME to the ALB and is covered by the ACM certificate. Empty means everything lives under `domain`."
  default     = "aws.octopaas.dev"
}

variable "alb_wait" {
  type        = string
  description = "How long to wait after the release before reading the Ingress for the ALB's hostname. The controller populates status.loadBalancer only once the ALB is provisioned; reading too early yields an empty list. Raise it if the apply fails on an empty hostname — do not remove the wait."
  default     = "120s"
}

# --- Cluster ---

variable "kubernetes_version" {
  type        = string
  description = <<-EOT
    EKS control-plane version. Keep this on a version in STANDARD support: once a
    version reaches extended support the control plane costs $0.60/hour instead of
    $0.10/hour — six times the figure this root's header quotes, for a cluster that
    is otherwise identical.

    Check before bumping, because the window moves:

      aws eks describe-cluster-versions --query \
        'clusterVersions[?versionStatus==`STANDARD_SUPPORT`].[clusterVersion,endOfStandardSupportDate]'
  EOT
  default     = "1.35"
}

variable "vpc_cidr" {
  type        = string
  description = "CIDR for the VPC. Subnets are carved out of it, two public and two private."
  default     = "10.70.0.0/16"
}

variable "node_instance_types" {
  type        = list(string)
  description = "Node instance types. t3.medium is too tight once the bundled Postgres asks for 500m/2Gi alongside kube-system, the ALB controller and the EBS CSI driver."
  default     = ["t3.large"]
}

variable "node_desired_size" {
  type        = number
  description = "Nodes to run."
  default     = 2
}

variable "node_min_size" {
  type        = number
  description = "Autoscaling floor."
  default     = 1
}

variable "node_max_size" {
  type        = number
  description = "Autoscaling ceiling."
  default     = 3
}

variable "spot_nodes" {
  type        = bool
  description = "Use Spot capacity: far cheaper, reclaimable at two minutes' notice."
  default     = true
}

variable "private_nodes" {
  type        = bool
  description = "Put nodes in the private subnets, which requires a NAT gateway for egress (~$32/month plus data processing). Off by default: public nodes reach Docker Hub, ghcr.io and the EKS API directly, and the cluster is a test environment."
  default     = false
}

# --- Chart ---

variable "namespace" {
  type        = string
  description = "Namespace for the release."
  default     = "octo"
}

variable "chart_source" {
  type        = string
  description = "local installs ../../../helm straight from this checkout, so a chart edit is one apply away. oci installs the published chart from ghcr.io, which is what actually verifies a release artifact."
  default     = "local"

  validation {
    condition     = contains(["local", "oci"], var.chart_source)
    error_message = "chart_source must be \"local\" or \"oci\"."
  }
}

variable "chart_version" {
  type        = string
  description = "Chart version to install when chart_source = oci. Ignored for a local path."
  default     = null
}

variable "image_values_file" {
  type        = string
  description = "Values file naming the images to run, as rendered by `task images:ttl`. Empty falls through to the chart's public Docker Hub defaults at its appVersion — correct only when the chart and the published images are from the same release."
  default     = ""
}

variable "helm_timeout" {
  type        = number
  description = "Helm install/upgrade timeout in seconds."
  default     = 900
}

# --- Database ---

variable "external_database" {
  type        = bool
  description = "Use a Terraform-provisioned RDS instance instead of the chart's bundled Postgres StatefulSet. The one flip between the two database paths."
  default     = false
}

variable "rds_instance_class" {
  type        = string
  description = "RDS instance class when external_database is on."
  default     = "db.t4g.micro"
}

# --- Editor authentication ---
#
# The Auth.js session secret and the KV encryption key are not inputs: both are
# minted per cluster by this root (main.tf) and held in this root's state, because
# neither has anything to stay consistent with across a destroy/recreate cycle.

variable "oidc_enabled" {
  type        = bool
  description = "Require OIDC single sign-on to reach the editor. Off by default, which suits a cluster that exists for an afternoon — but it means anyone who can resolve the hostname gets in, so turn it on for anything left standing on a public domain. Needs oidc_client_id and oidc_client_secret."
  default     = false
}

variable "oidc_issuer" {
  type        = string
  description = "OIDC issuer URL (AUTH_EETR_ISSUER)."
  default     = "https://auth.eetr.app"
}

variable "oidc_client_id" {
  type        = string
  description = "OIDC client id. Not a secret — it travels in the authorization redirect."
  default     = ""
}

variable "oidc_client_secret" {
  type        = string
  description = "OIDC client secret."
  default     = ""
  sensitive   = true
}

variable "oidc_write_roles" {
  type        = string
  description = "Comma-separated roles allowed to perform writes (e.g. \"admin,operator\"). Empty lets any signed-in user write."
  default     = ""
}

variable "oidc_roles_claim" {
  type        = string
  description = "id-token claim carrying roles. Empty uses the Auth.js default, \"roles\"."
  default     = ""
}


variable "tags" {
  type        = map(string)
  description = "Tags applied to every AWS resource created here."
  default     = {}
}
