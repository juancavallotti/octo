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
    # Deploy step: roll the cluster after publishing (off unless enable_deploy).
    _DEPLOY   = var.enable_deploy ? "true" : "false"
    _INSTANCE = var.instance_name
    _ZONE     = var.zone
    _DOMAIN   = var.domain
  }

  depends_on = [google_project_service.cloudbuild]
}

# --- Permissions the build's deploy step needs (only when enable_deploy) ---
# Least-privilege, gated so a publish-only setup stays minimal.

# Read the Postgres password (release data source).
resource "google_secret_manager_secret_iam_member" "deploy_secret" {
  count     = var.enable_deploy ? 1 : 0
  project   = var.project_id
  secret_id = var.deploy_secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = local.cloudbuild_sa
}

# Read/write the release Terraform state in GCS.
resource "google_storage_bucket_iam_member" "deploy_state" {
  count  = var.enable_deploy ? 1 : 0
  bucket = var.state_bucket
  role   = "roles/storage.objectAdmin"
  member = local.cloudbuild_sa
}

# SSH to the VM (octo-pull + kubeconfig fetch) and tunnel through IAP.
resource "google_project_iam_member" "deploy_compute" {
  count   = var.enable_deploy ? 1 : 0
  project = var.project_id
  role    = "roles/compute.instanceAdmin.v1"
  member  = local.cloudbuild_sa
}

resource "google_project_iam_member" "deploy_iap" {
  count   = var.enable_deploy ? 1 : 0
  project = var.project_id
  role    = "roles/iap.tunnelResourceAccessor"
  member  = local.cloudbuild_sa
}

# Act as the VM service account so `gcloud compute ssh` can push its key.
resource "google_service_account_iam_member" "deploy_actas" {
  count              = var.enable_deploy ? 1 : 0
  service_account_id = "projects/${var.project_id}/serviceAccounts/${var.vm_service_account_email}"
  role               = "roles/iam.serviceAccountUser"
  member             = local.cloudbuild_sa
}
