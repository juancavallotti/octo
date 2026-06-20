variable "project_id" {
  type        = string
  description = "GCP project that hosts the Terraform state bucket."
}

variable "region" {
  type        = string
  description = "Location for the state bucket (a region keeps it close to the rest of the infra)."
  default     = "us-west1"
}

variable "bucket_name" {
  type        = string
  description = "State bucket name. Empty -> octo-tfstate-{project_id}. Bucket names are globally unique, so override if that is taken."
  default     = ""
}
