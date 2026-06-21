# --- Shared values (live in ../octo.tfvars, also consumed by the release root) ---

variable "project_id" {
  type        = string
  description = "GCP project ID to deploy into."
}

variable "region" {
  type        = string
  description = "GCP region for the static IP, zonal resources, and the Artifact Registry docker host."
  default     = "us-west1"
}

variable "zone" {
  type        = string
  description = "GCP zone for the GCE instance."
  default     = "us-west1-a"
}

variable "instance_name" {
  type        = string
  description = "Name for the GCE instance and related resources."
  default     = "octo"
}

variable "repository_id" {
  type        = string
  description = "Artifact Registry repository id. Hosts both the octo Docker images and the OCI Helm chart."
  default     = "octo"
}

variable "domain" {
  type        = string
  description = "Fully-qualified hostname the editor is served on (cert-manager issues a Let's Encrypt cert for it). Per-integration subdomains live under *.{domain}."
  default     = "octo.juancavallotti.com"
}

# --- Infra-only values (also set in the shared octo.tfvars) ---

variable "dns_managed_zone" {
  type        = string
  description = "Name (not DNS name) of the existing Cloud DNS managed zone."
}

variable "machine_type" {
  type        = string
  description = "GCE machine type. octo runs editor + orchestrator + postgres + cert-manager + traefik plus integration pods; e2-standard-2 (8GB) is the comfortable floor."
  default     = "e2-standard-2"
}

variable "acme_email" {
  type        = string
  description = "Email for the Let's Encrypt ACME account (expiry notices)."
  default     = "juancavallotti@gmail.com"
}

variable "boot_disk_size_gb" {
  type        = number
  description = "Boot disk size in GB (holds k3s, images, and local-path Postgres data)."
  default     = 30
}

variable "ssh_source_ranges" {
  type        = list(string)
  description = "CIDR ranges allowed to reach SSH (port 22). Restrict to your IP; default is open."
  default     = ["0.0.0.0/0"]
}

variable "kube_api_source_ranges" {
  type        = list(string)
  description = "CIDR ranges allowed to reach the k3s API (6443), needed by the Terraform Helm release. null = reuse ssh_source_ranges."
  default     = null
}

variable "enable_cloudbuild" {
  type        = bool
  description = "Create the Cloud Build trigger + IAM. Requires the GitHub repo to be connected to Cloud Build first (console, one-time). Leave false on the first apply, then flip to true."
  default     = false
}

variable "github_owner" {
  type        = string
  description = "GitHub owner of the repo the Cloud Build trigger watches."
  default     = "juancavallotti"
}

variable "github_repo" {
  type        = string
  description = "GitHub repository name the Cloud Build trigger watches."
  default     = "eip-go"
}

variable "cloudbuild_auto_deploy" {
  type        = bool
  description = "When the trigger exists, also let the build roll the cluster on a tag (sets _DEPLOY=true and grants the build SA the deploy permissions). Only effective with enable_cloudbuild = true."
  default     = true
}

variable "state_bucket" {
  type        = string
  description = "GCS bucket backing the release Terraform state (created by `task state:bucket`). Empty -> octo-tfstate-{project_id}. Used to scope the build SA's storage access."
  default     = ""
}

# --- OIDC SSO (consumed by the release root) ---
# Declared here only so the shared octo.tfvars carries them without "undeclared
# variable" warnings on an infra apply; the infra root creates nothing for SSO.

variable "oidc_enabled" {
  type        = bool
  description = "Release-only: enable OIDC SSO for the editor. Ignored by the infra root."
  default     = false
}

variable "oidc_issuer" {
  type        = string
  description = "Release-only: OIDC issuer URL (eetr). Ignored by the infra root."
  default     = "https://auth.eetr.app"
}

variable "oidc_client_id" {
  type        = string
  description = "Release-only: OIDC client id (non-secret). Ignored by the infra root."
  default     = ""
}

variable "oidc_client_secret" {
  type        = string
  description = "Release-only: OIDC client secret. Ignored by the infra root."
  default     = ""
  sensitive   = true
}

variable "oidc_write_roles" {
  type        = string
  description = "Release-only: comma-separated roles allowed to write; empty = any signed-in user. Ignored by the infra root."
  default     = ""
}

variable "oidc_roles_claim" {
  type        = string
  description = "Release-only: id-token claim carrying roles. Ignored by the infra root."
  default     = ""
}
