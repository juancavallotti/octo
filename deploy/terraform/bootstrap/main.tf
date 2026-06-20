# GCS bucket that holds the remote Terraform state for the infra and release roots.
# A backend bucket cannot live in the state it backs, so it is provisioned here in a
# tiny standalone root applied once. Its own state stays local — this never changes,
# and the bucket is trivial to import if the local state is lost.
#
#   cd deploy/terraform/bootstrap && terraform init && terraform apply

resource "google_storage_bucket" "tfstate" {
  name     = coalesce(var.bucket_name, "octo-tfstate-${var.project_id}")
  location = var.region

  # Recover from a bad apply: keep prior state versions.
  versioning {
    enabled = true
  }

  # Required for the IAM model the Cloud Build deploy SA uses (objectAdmin on the bucket).
  uniform_bucket_level_access = true

  # Cap how many old state versions we retain so the bucket doesn't grow unbounded.
  lifecycle_rule {
    condition {
      num_newer_versions = 10
    }
    action {
      type = "Delete"
    }
  }
}
