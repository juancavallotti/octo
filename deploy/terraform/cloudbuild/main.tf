# One-time Cloud Build setup so pushing octo's images + Helm chart is automated:
# every version tag fires the build defined in cloudbuild.yaml. Apply once, after
# deploy/terraform/registry has created the Artifact Registry repository and after
# the repo is connected to Cloud Build (see the module's note).

module "cloudbuild" {
  source = "../modules/cloudbuild"

  project_id    = var.project_id
  region        = var.region
  repository_id = var.repository_id
  github_owner  = var.github_owner
  github_repo   = var.github_repo
  tag_pattern   = var.tag_pattern
}
