variable "project_id" {
  type        = string
  description = "GCP project that runs Cloud Build and hosts the Artifact Registry repository."
}

variable "region" {
  type        = string
  description = "Artifact Registry region (also the docker host prefix, e.g. us-west1 -> us-west1-docker.pkg.dev). Passed to the build as _REGION."
  default     = "us-west1"
}

variable "repository_id" {
  type        = string
  description = "Artifact Registry repository the build pushes images and the chart into. Created by deploy/terraform/registry."
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
  description = "Regex of git tags that fire the build. Defaults to version tags (release-please publishes vX.Y.Z)."
  default     = "^v.*$"
}

variable "trigger_name" {
  type        = string
  description = "Name of the Cloud Build trigger."
  default     = "octo-publish"
}

variable "build_config" {
  type        = string
  description = "Path to the Cloud Build config in the repo."
  default     = "cloudbuild.yaml"
}
