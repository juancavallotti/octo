variable "project_id" {
  type        = string
  description = "GCP project that hosts the Artifact Registry repository."
}

variable "region" {
  type        = string
  description = "Region for the Artifact Registry repository (also the docker host prefix, e.g. us-west1 -> us-west1-docker.pkg.dev)."
  default     = "us-west1"
}

variable "repository_id" {
  type        = string
  description = "Artifact Registry repository id. Hosts both the octo Docker images and the OCI Helm chart."
  default     = "octo"
}
