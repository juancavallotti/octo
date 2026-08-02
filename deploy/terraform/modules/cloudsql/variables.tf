variable "project_id" {
  type        = string
  description = "GCP project to create the instance in."
}

variable "name" {
  type        = string
  description = "Cloud SQL instance name. Note that Google reserves a deleted instance name for about a week, so a destroy/recreate cycle within that window needs a different name."
}

variable "region" {
  type        = string
  description = "Region for the instance. Must be the region of the VPC the cluster lives in."
  default     = "us-west1"
}

variable "network_id" {
  type        = string
  description = "Self-link of the VPC to attach the private IP to (modules/gke-cluster outputs network_id). The service-networking peering on that VPC must already exist — order this module after it."
}

variable "database_version" {
  type        = string
  description = "Cloud SQL Postgres version. Matches the bundled postgres:16-alpine the chart otherwise runs."
  default     = "POSTGRES_16"
}

variable "tier" {
  type        = string
  description = "Machine tier. db-g1-small is the cheapest that reliably runs Postgres 16; db-f1-micro is smaller but tight enough to be flaky under the schema Job."
  default     = "db-g1-small"
}

variable "disk_size_gb" {
  type        = number
  description = "Initial data disk size. Autoresize is on, and shrinking is not possible, so start small."
  default     = 10
}

variable "disk_type" {
  type        = string
  description = "PD_SSD or PD_HDD."
  default     = "PD_SSD"
}

variable "database_name" {
  type        = string
  description = "Database the chart connects to."
  default     = "octo"
}

variable "database_user" {
  type        = string
  description = "Database user. It owns database_name, which is required for the chart's schema Job to create tables on Postgres 16 — see the comment on google_sql_user."
  default     = "octo"
}

variable "high_availability" {
  type        = bool
  description = "REGIONAL availability: a synchronous standby in a second zone, at roughly double the cost. Off for test instances."
  default     = false
}

variable "backup_enabled" {
  type        = bool
  description = "Automated backups plus point-in-time recovery. Off for test instances, which are destroyed with the cluster."
  default     = false
}

variable "query_insights" {
  type        = bool
  description = "Enable Cloud SQL query insights."
  default     = false
}

variable "deletion_protection" {
  type        = bool
  description = "Refuse to delete the instance. Off by default so `terraform destroy` completes — turn it on for anything holding data you care about."
  default     = false
}
