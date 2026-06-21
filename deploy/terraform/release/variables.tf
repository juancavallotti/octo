variable "project_id" {
  type        = string
  description = "GCP project (for the OCI chart token and SSH image pulls)."
}

variable "region" {
  type        = string
  description = "Artifact Registry region."
  default     = "us-west1"
}

variable "zone" {
  type        = string
  description = "Zone of the k3s VM (for the SSH image pull)."
  default     = "us-west1-a"
}

variable "instance_name" {
  type        = string
  description = "Name of the k3s VM (for the SSH image pull)."
  default     = "octo"
}

variable "repository_id" {
  type        = string
  description = "Artifact Registry repository holding the images and OCI chart."
  default     = "octo"
}

variable "domain" {
  type        = string
  description = "Editor hostname; must match the k3s deployment. Per-integration subdomains live under *.{domain}."
  default     = "octo.juancavallotti.com"
}

variable "image_tag" {
  type        = string
  description = "Tag of the octo-* images to deploy (e.g. v0.1.1 or latest). Changing it re-pulls on the node and rolls the Deployments."
  default     = "latest"
}

variable "chart_version" {
  type        = string
  description = "Version of the octo chart in Artifact Registry OCI. Must match helm/Chart.yaml at the published release; derived from it by Cloud Build and `task deploy`, so it is required (no default) to avoid drift."
}

variable "cluster_issuer" {
  type        = string
  description = "Per-host (HTTP-01) cert-manager ClusterIssuer created by the k3s bootstrap; used when wildcard_tls is false."
  default     = "letsencrypt-prod"
}

variable "wildcard_tls" {
  type        = bool
  description = "Issue one *.{domain} wildcard cert via DNS-01 and share it across the editor + per-integration ingresses, so subdomains validate. Requires the bootstrap's DNS-01 ClusterIssuer."
  default     = true
}

variable "kubeconfig" {
  type        = string
  description = "Path to the k3s kubeconfig (server rewritten to https://{domain}:6443). Produced by `task deploy:kubeconfig`."
  default     = ""
}

# Declared (unused here) so the single shared octo.tfvars — which carries these for
# the infra root — does not emit "undeclared variable" warnings on a release apply.
variable "dns_managed_zone" {
  type        = string
  description = "Infra-only (Cloud DNS zone name); ignored by the release root."
  default     = ""
}

variable "enable_cloudbuild" {
  type        = bool
  description = "Infra-only (creates the Cloud Build trigger); ignored by the release root."
  default     = false
}

# --- OIDC SSO (shared octo.tfvars) ---

variable "oidc_enabled" {
  type        = bool
  description = "Enable OIDC single sign-on for the editor."
  default     = false
}

variable "oidc_issuer" {
  type        = string
  description = "OIDC issuer URL (the eetr identity provider)."
  default     = "https://auth.eetr.app"
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

variable "oidc_client_id" {
  type        = string
  description = "OIDC client id from the IdP (non-secret); passed to the chart."
  default     = ""
}

variable "oidc_client_secret" {
  type        = string
  description = "OIDC client secret from the IdP; passed to the chart (lands in release state, not Secret Manager)."
  default     = ""
  sensitive   = true
}
