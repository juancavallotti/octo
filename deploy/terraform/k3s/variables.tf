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

variable "machine_type" {
  type        = string
  description = "GCE machine type. octo runs editor + orchestrator + postgres + cert-manager + traefik plus integration pods; e2-standard-2 (8GB) is the comfortable floor."
  default     = "e2-standard-2"
}

variable "instance_name" {
  type        = string
  description = "Name for the GCE instance and related resources."
  default     = "octo"
}

variable "domain" {
  type        = string
  description = "Fully-qualified hostname the editor is served on (cert-manager issues a Let's Encrypt cert for it). Per-integration subdomains live under *.{domain}."
  default     = "octo.juancavallotti.com"
}

variable "dns_managed_zone" {
  type        = string
  description = "Name (not DNS name) of the existing Cloud DNS managed zone."
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

variable "repository_id" {
  type        = string
  description = "Artifact Registry repository (created by deploy/terraform/registry) holding the images and OCI chart."
  default     = "octo"
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
