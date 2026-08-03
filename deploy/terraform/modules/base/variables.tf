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
  description = "Boot disk size in GB. Holds the OS, k3s and its image cache — not the database, which lives on the data disk below."
  default     = 30
}

# --- Data disk ---
# Attached separately from the boot disk so the database survives VM replacement.
# The caller's startup script is responsible for formatting and mounting it; this
# module only creates and attaches the device.

variable "data_disk_size_gb" {
  type        = number
  description = "Size of the Postgres data disk in GB. Growing it is an in-place update; the startup script runs resize2fs on the next boot to grow the filesystem to match."
  default     = 20
}

variable "data_disk_type" {
  type        = string
  description = "Persistent disk type for the data disk. pd-ssd for write-heavy installs."
  default     = "pd-balanced"
}

variable "data_disk_device_name" {
  type        = string
  description = "Device name the data disk is attached under; the guest sees it at /dev/disk/by-id/google-{this}. Must match what the startup script looks for."
  default     = "octo-data"
}

variable "data_disk_snapshot_retention_days" {
  type        = number
  description = "Days to keep daily snapshots of the data disk. 0 disables the schedule. Snapshots are kept when the disk is deleted, so they are the recovery path for an accidental destroy."
  default     = 7
}

variable "data_disk_snapshot_start_time" {
  type        = string
  description = "UTC start time (HH:MM) for the daily data-disk snapshot."
  default     = "08:00"
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
