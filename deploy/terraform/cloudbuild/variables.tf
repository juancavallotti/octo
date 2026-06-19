variable "project_id" {
  type        = string
  description = "GCP project that runs Cloud Build and hosts the Artifact Registry repository."
}

variable "region" {
  type        = string
  description = "Region for Artifact Registry / the docker host prefix."
  default     = "us-west1"
}

variable "repository_id" {
  type        = string
  description = "Artifact Registry repository created by deploy/terraform/registry."
  default     = "octo"
}

variable "github_owner" {
  type        = string
  description = "GitHub owner of the repo the trigger watches."
  default     = "juancavallotti"
}

variable "github_repo" {
  type        = string
  description = "GitHub repository name the trigger watches."
  default     = "eip-go"
}

variable "tag_pattern" {
  type        = string
  description = "Regex of git tags that fire the build (release-please publishes vX.Y.Z)."
  default     = "^v.*$"
}
