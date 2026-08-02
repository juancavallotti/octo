# Variables for the `infra` root. Values are supplied by terraform.tfvars, which
# Terraform loads automatically — see terraform.tfvars.example.
#
# project_id / domain / apps_domain are also set on the release root. Each root
# declares only what it uses, so a stale or misspelled name is a hard error rather
# than a warning nobody reads.

# --- Shared with the release root (must agree) ---

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
  description = "Fully-qualified hostname the editor is served on (cert-manager issues a Let's Encrypt cert for it). Per-integration subdomains live under *.{apps_domain}, which defaults to this domain — see apps_domain."
  default     = "octo.juancavallotti.com"
}

# apps_domain lets per-integration subdomains live under a different,
# Cloud-DNS-delegated domain than the editor's own host — e.g. when domain's
# registrar won't allow delegating the whole zone (Cloudflare Registrar, for one)
# but a subdomain of it can still be NS-delegated to Cloud DNS. Empty (the
# default) keeps the original single-domain behavior: everything lives under
# `domain`, which must then itself be a Cloud DNS zone Terraform can manage.
variable "apps_domain" {
  type        = string
  description = "Base hostname per-integration subdomains live under ({slug}.{apps_domain}) and the domain the wildcard cert covers. Empty = same as `domain`."
  default     = ""
}

# --- Infra-only ---

variable "dns_managed_zone" {
  type        = string
  description = "Name (not DNS name) of the existing Cloud DNS managed zone backing apps_domain (or domain, when apps_domain is unset)."
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
  description = "Boot disk size in GB (holds the OS, k3s and its image cache). Postgres does NOT live here — see data_disk_size_gb."
  default     = 30
}

# --- Data disk ---
# A separate persistent disk for the database, mounted at data_disk_mount_path by
# the startup script. Postgres is pinned to {mount}/postgres via the release root's
# postgres_host_path, so it survives the VM being replaced or rebuilt.

variable "data_disk_size_gb" {
  type        = number
  description = "Size of the Postgres data disk in GB. Growing it is in-place; the next reboot grows the filesystem to match."
  default     = 20
}

variable "data_disk_type" {
  type        = string
  description = "Persistent disk type for the data disk. pd-ssd for write-heavy installs."
  default     = "pd-balanced"
}

variable "data_disk_device_name" {
  type        = string
  description = "Device name the data disk is attached under; the guest sees /dev/disk/by-id/google-{this}."
  default     = "octo-data"
}

variable "data_disk_mount_path" {
  type        = string
  description = "Where the startup script mounts the data disk. The release root's postgres_host_path must be a path underneath it."
  default     = "/mnt/octo-data"
}

variable "data_disk_snapshot_retention_days" {
  type        = number
  description = "Days to keep daily snapshots of the data disk; 0 disables them. Snapshots outlive the disk, so they are the recovery path if it is ever destroyed."
  default     = 7
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
  default     = "octo"
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
