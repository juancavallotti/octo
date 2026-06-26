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
  description = "cert-manager ClusterIssuer for per-host (HTTP-01) TLS, used when wildcard_tls is false."
  default     = "letsencrypt-prod"
}

variable "wildcard_tls" {
  type        = bool
  description = "Issue one *.{domain} wildcard cert via DNS-01 and have the editor + per-integration ingresses share it (so subdomains validate). Requires the DNS-01 ClusterIssuer and DNS admin on the zone."
  default     = true
}

variable "wildcard_cluster_issuer" {
  type        = string
  description = "DNS-01 cert-manager ClusterIssuer that issues the wildcard cert (created by the VM bootstrap)."
  default     = "letsencrypt-dns"
}

variable "timeout" {
  type        = number
  description = "Helm install/upgrade timeout in seconds."
  default     = 600
}

# --- OIDC SSO ---

variable "oidc_enabled" {
  type        = bool
  description = "Enable OIDC SSO in the chart (creates the auth Secret + editor env)."
  default     = false
}

variable "oidc_issuer" {
  type        = string
  description = "OIDC issuer URL passed to the editor (AUTH_EETR_ISSUER)."
  default     = "https://auth.eetr.app"
}

variable "oidc_client_id" {
  type        = string
  description = "OIDC client id (non-secret)."
  default     = ""
}

variable "oidc_client_secret" {
  type        = string
  description = "OIDC client secret."
  default     = ""
  sensitive   = true
}

variable "auth_secret" {
  type        = string
  description = "Auth.js session secret (AUTH_SECRET)."
  default     = ""
  sensitive   = true
}

variable "oidc_write_roles" {
  type        = string
  description = "Comma-separated roles allowed to perform writes; empty = any signed-in user."
  default     = ""
}

variable "oidc_roles_claim" {
  type        = string
  description = "id-token claim carrying roles (Auth.js default \"roles\")."
  default     = ""
}

variable "kv_encryption_key" {
  type        = string
  description = "Base64-encoded 32-byte AES-256 key for encrypting KV secret namespaces at rest. Empty disables encryption (secret writes rejected, plain KV still works)."
  default     = ""
  sensitive   = true
}
