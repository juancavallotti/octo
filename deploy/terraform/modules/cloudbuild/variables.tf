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

# --- Deploy step (the build rolls the cluster after publishing) ---

variable "enable_deploy" {
  type        = bool
  description = "Whether the build's deploy step runs (sets the _DEPLOY substitution) and the build SA gets the deploy permissions. The cluster (infra) must exist first."
  default     = false
}

variable "instance_name" {
  type        = string
  description = "k3s VM name the deploy step SSHes to (passed as _INSTANCE)."
  default     = "octo"
}

variable "zone" {
  type        = string
  description = "Zone of the k3s VM (passed as _ZONE)."
  default     = "us-west1-a"
}

variable "domain" {
  type        = string
  description = "Editor hostname; the deploy step rewrites the kubeconfig server to https://{domain}:6443 (passed as _DOMAIN)."
  default     = "octo.juancavallotti.com"
}

variable "state_bucket" {
  type        = string
  description = "GCS bucket backing the release Terraform state (objectAdmin granted to the build SA). Required when enable_deploy = true."
  default     = ""
}

variable "vm_service_account_email" {
  type        = string
  description = "Email of the k3s VM service account; the build SA is granted serviceAccountUser on it for SSH. Required when enable_deploy = true."
  default     = ""
}
