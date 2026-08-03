# Remote state, versioned, shared bucket — one prefix per root. Not local state:
# losing it orphans a cluster, a VPC and possibly a Cloud SQL instance, with
# nothing left able to destroy them.
#
# The bucket is supplied at init time (-backend-config=../backend.hcl) because a
# backend block cannot reference variables; the prefix is a literal because it
# identifies this root and must not be shared with another.
terraform {
  backend "gcs" {
    prefix = "gke-autopilot"
  }
}
