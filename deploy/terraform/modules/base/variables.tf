# Shared infrastructure for a single-VM deployment. Workload-agnostic: the caller
# supplies the VM startup script, any file metadata, and the .env secret contents.

variable "project_id" {
  type        = string
  description = "GCP project ID to deploy into."
}

variable "region" {
  type        = string
  description = "GCP region for the static IP."
  default     = "us-west1"
}

variable "zone" {
  type        = string
  description = "GCP zone for the GCE instance."
  default     = "us-west1-a"
}

variable "machine_type" {
  type        = string
  description = "GCE machine type for the VM."
  default     = "e2-standard-2"
}

variable "instance_name" {
  type        = string
  description = "Name for the GCE instance and related resources (also the network tag)."
  default     = "octo"
}

variable "domain" {
  type        = string
  description = "Fully-qualified hostname served on the VM; the A record points here."
}

variable "wildcard_dns" {
  type        = bool
  description = "Also create a *.{domain} A record to the same IP (per-integration subdomains, Stage 2)."
  default     = true
}

variable "dns_managed_zone" {
  type        = string
  description = "Name (not DNS name) of the existing Cloud DNS managed zone."
}

variable "ssh_source_ranges" {
  type        = list(string)
  description = "CIDR ranges allowed to reach SSH (port 22)."
  default     = ["0.0.0.0/0"]
}

variable "web_tcp_ports" {
  type        = list(string)
  description = "TCP ports opened to the world for the workload."
  default     = ["80", "443"]
}

variable "kube_api_source_ranges" {
  type        = list(string)
  description = "CIDR ranges allowed to reach the k3s API (6443). Empty = no rule (API stays node-local). Restrict to operator IPs when Terraform manages the Helm release remotely."
  default     = []
}

variable "boot_disk_size_gb" {
  type        = number
  description = "Boot disk size in GB."
  default     = 30
}

variable "startup_script" {
  type        = string
  description = "Rendered startup script the VM runs on first boot."
}

variable "metadata" {
  type        = map(string)
  description = "Extra instance metadata (e.g. workload config files the startup script fetches)."
  default     = {}
}
