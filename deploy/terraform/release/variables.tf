variable "project_id" {
  type        = string
  description = "GCP project (for the OCI chart token, the Postgres-password secret, and SSH image pulls)."
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
  description = "Name of the k3s VM (for SSH) and the source of the Postgres-password secret id."
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
  description = "cert-manager ClusterIssuer created by the k3s bootstrap."
  default     = "letsencrypt-prod"
}

variable "kubeconfig" {
  type        = string
  description = "Path to the k3s kubeconfig (server rewritten to https://{domain}:6443). Produced by `task deploy:kubeconfig`."
  default     = ""
}

variable "secret_id" {
  type        = string
  description = "Secret Manager secret holding the Postgres password. null = {instance_name}-postgres."
  default     = null
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
