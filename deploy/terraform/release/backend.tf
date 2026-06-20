# Remote state in GCS so the operator and the Cloud Build deploy step share one
# release state. Create the bucket once with `task state:bucket PROJECT=...`. The
# bucket name is a literal (backend blocks cannot interpolate variables) and must be
# octo-tfstate-{project_id}; change it here if your project id differs.
terraform {
  backend "gcs" {
    bucket = "octo-tfstate-juancavallotti"
    prefix = "release"
  }
}
