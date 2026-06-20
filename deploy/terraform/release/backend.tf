# Remote state in GCS so the operator and the Cloud Build deploy step share one
# release state (created by deploy/terraform/bootstrap). The bucket name is a literal
# — backend blocks cannot interpolate variables; it must match the bootstrap bucket
# (octo-tfstate-{project_id}). Migrate existing local state with:
#   terraform init -migrate-state
terraform {
  backend "gcs" {
    bucket = "octo-tfstate-juancavallotti"
    prefix = "release"
  }
}
