# Remote state, versioned, shared bucket — one prefix per root. This root's state
# holds the generated Postgres password, the Auth.js session secret and the KV
# encryption key, so Cloud Build and a laptop must read the same copy.
#
# The bucket is supplied at init time (-backend-config=../backend.hcl) because a
# backend block cannot reference variables; the prefix is a literal because it
# identifies this root and must not be shared with another.
terraform {
  backend "gcs" {
    prefix = "release"
  }
}
