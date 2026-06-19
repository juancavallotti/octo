# One-time Cloud Build setup: a trigger that builds + pushes octo's images and
# OCI Helm chart to Artifact Registry on version tags, plus the IAM grant the
# build needs to write to the repository.
#
# Prerequisite (manual, one-time): connect the GitHub repo to Cloud Build by
# installing the Cloud Build GitHub App on it
# (https://console.cloud.google.com/cloud-build/triggers → Connect repository).
# Terraform creates the trigger but cannot perform the GitHub OAuth handshake.

resource "google_project_service" "cloudbuild" {
  service            = "cloudbuild.googleapis.com"
  disable_on_destroy = false
}

data "google_project" "this" {
  project_id = var.project_id
}

locals {
  image_base = "${var.region}-docker.pkg.dev/${var.project_id}/${var.repository_id}"
  # Cloud Build's default service account runs the build.
  cloudbuild_sa = "serviceAccount:${data.google_project.this.number}@cloudbuild.gserviceaccount.com"
}

# Let the build push images and the chart into the repository.
resource "google_artifact_registry_repository_iam_member" "writer" {
  project    = var.project_id
  location   = var.region
  repository = var.repository_id
  role       = "roles/artifactregistry.writer"
  member     = local.cloudbuild_sa
}

resource "google_cloudbuild_trigger" "publish" {
  name        = var.trigger_name
  description = "Build and push octo images + Helm chart to Artifact Registry on version tags."
  filename    = var.build_config

  github {
    owner = var.github_owner
    name  = var.github_repo
    push {
      tag = var.tag_pattern
    }
  }

  substitutions = {
    _IMAGE_BASE = local.image_base
    _REGION     = var.region
    # Built-in: the pushed git tag (e.g. v0.1.1) becomes the image tag.
    _TAG = "$TAG_NAME"
  }

  depends_on = [google_project_service.cloudbuild]
}
