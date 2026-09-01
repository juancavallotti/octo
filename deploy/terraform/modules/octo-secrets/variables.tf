variable "namespace" {
  type        = string
  description = "Namespace the release is installed into, and the one every Secret is created in. A Secret is only readable by workloads in its own namespace, so this must be the namespace the chart is installed into and not merely near it."
}

variable "create_namespace" {
  type        = bool
  description = "Create the namespace here rather than letting Helm create it. True is the normal answer: a Secret has to exist before the chart that references it installs, and a namespace Helm has not created yet is nowhere to put one. Set false when the caller already has a kubernetes_namespace_v1 of its own — then pass create_namespace = false to the helm-release module as well, or Helm will try to create a namespace that exists."
  default     = true
}

variable "name_prefix" {
  type        = string
  description = "Prefix for every Secret this module creates, so an installation's credentials are recognisable as a set and two releases in one namespace do not collide. The release name is the obvious value."
  default     = "octo"
}

# --- The credentials ---
#
# Every one of these is optional and empty means "create no Secret": an
# installation with no embedding server has no provider key, and one on a managed
# database has no bundled-Postgres password. The corresponding output is null, and
# the helm-release module emits nothing for it — so the chart falls back to
# whatever a values file said, which is how a values profile stays able to decide.
#
# Values, not references. This module is the boundary: a credential is a Terraform
# value on this side of it and a Secret reference on the other, and nothing
# downstream of here — no Helm value, no release history entry — carries the value
# itself. It still lives in this root's state, which is the trade the whole setup
# makes explicitly (there is no Secret Manager); state is one place, encrypted at
# rest in its bucket, and rotatable by re-applying. Helm release history is every
# retained revision, in the cluster, readable by anyone who can read Secrets in the
# namespace, and outlives the value it held.

variable "postgres_password" {
  type        = string
  description = "Password for the chart's bundled Postgres. Empty on an external-database install, which has no chart-owned password."
  default     = ""
  sensitive   = true
}

variable "auth_secret" {
  type        = string
  description = "Auth.js session secret (AUTH_SECRET). Empty unless SSO is enabled — the editor reads it nowhere else."
  default     = ""
  sensitive   = true
}

variable "oidc_client_secret" {
  type        = string
  description = "OIDC client secret issued by your identity provider. Shares a Secret with auth_secret: the chart reads both from one reference, and they are enabled and disabled together."
  default     = ""
  sensitive   = true
}

variable "kv_encryption_key" {
  type        = string
  description = "Base64-encoded 32-byte AES-256 key encrypting KV secret namespaces at rest. Empty leaves encryption off — secret-namespace writes are rejected and plain KV still works. Never rotate it on an installation that has written one: a new key makes every existing value unreadable."
  default     = ""
  sensitive   = true
}

variable "dev_run_hash_secret" {
  type        = string
  description = "HMAC key deriving every dev run's identity and public hostname. Required by the chart whenever dev runs are on, which is the default. Never rotate it: a new key re-labels every exposed dev run and silently stops delivery of webhooks registered against the old hostnames."
  default     = ""
  sensitive   = true
}

variable "embeddings_api_key" {
  type        = string
  description = "Provider API key for the embedding server. Empty deploys no embedding server, which is an ordinary way to run — agent-memory search then ranks by matching text rather than by meaning."
  default     = ""
  sensitive   = true
}
