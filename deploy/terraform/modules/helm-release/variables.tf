variable "release_name" {
  type        = string
  description = "Helm release name."
  default     = "octo"
}

variable "namespace" {
  type        = string
  description = "Namespace for the release."
  default     = "octo-dev"
}

variable "image_base" {
  type        = string
  description = "Artifact Registry base, e.g. us-west1-docker.pkg.dev/PROJECT/octo. Used both as the OCI chart repo and the chart's image.registry."
}

variable "chart_version" {
  type        = string
  description = "Version of the octo chart in Artifact Registry OCI (matches helm/Chart.yaml)."
}

variable "image_tag" {
  type        = string
  description = "Tag of the octo-* images to run (e.g. v0.1.1 or latest)."
}

variable "registry_username" {
  type        = string
  description = "Username for OCI chart pulls (oauth2accesstoken for Artifact Registry)."
  default     = "oauth2accesstoken"
}

variable "registry_password" {
  type        = string
  description = "Token/password for OCI chart pulls (a GCP access token for Artifact Registry)."
  sensitive   = true
}

variable "domain" {
  type        = string
  description = "Editor hostname; per-integration subdomains live under *.{domain}."
}

variable "postgres_password" {
  type        = string
  description = "Postgres password passed to the chart."
  sensitive   = true
}

variable "cluster_issuer" {
  type        = string
  description = "cert-manager ClusterIssuer for editor + per-integration TLS."
  default     = "letsencrypt-prod"
}

variable "timeout" {
  type        = number
  description = "Helm install/upgrade timeout in seconds."
  default     = 600
}
