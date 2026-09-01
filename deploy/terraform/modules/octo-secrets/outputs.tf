# One output per credential, each a {name, key} reference or null when the
# credential's create_* flag is false. Null rather than an empty object so a caller
# can hand it straight to modules/helm-release, whose reference variables read an
# empty name as "emit nothing" and leave the chart's values file to decide.
#
# Every value here is known during PLAN — the names come from locals rather than
# from the resources' own attributes, and the flags are plan-known booleans. That
# matters because callers feed these into `for_each` on the Helm side, where an
# unknown is a hard planning error rather than something Terraform waits out.
#
# The cost of that is no implicit ordering: nothing here refers to a resource, so
# nothing makes Helm wait for the Secrets. Callers say so explicitly with
# depends_on, and must, because a Secret has to exist before the chart reads it.

output "namespace" {
  description = "The namespace the Secrets were created in. Pass it to the helm-release module together with create_namespace = false — and with depends_on this module, which is what actually orders the two."
  value       = local.namespace
}

output "postgres" {
  description = "Reference to the bundled-Postgres password Secret, or null when create_postgres_secret is false."
  value = var.create_postgres_secret ? {
    name = local.postgres_secret_name
    key  = "postgres-password"
  } : null
}

output "auth" {
  description = "Reference to the editor's auth Secret, carrying both the Auth.js session secret and the OIDC client secret. Null when create_auth_secret is false."
  value = var.create_auth_secret ? {
    name              = local.auth_secret_name
    auth_secret_key   = "auth-secret"
    client_secret_key = "oidc-client-secret"
  } : null
}

output "kv" {
  description = "Reference to the KV encryption key Secret, or null when create_kv_secret is false."
  value = var.create_kv_secret ? {
    name = local.kv_secret_name
    key  = "kv-encryption-key"
  } : null
}

output "dev_runs" {
  description = "Reference to the dev-run HMAC key Secret, or null when create_dev_runs_secret is false — in which case the chart refuses to render while dev runs are enabled."
  value = var.create_dev_runs_secret ? {
    name = local.dev_runs_secret_name
    key  = "dev-run-hash-secret"
  } : null
}

output "embeddings" {
  description = "Reference to the embedding server's provider-key Secret, or null when create_embeddings_secret is false."
  value = var.create_embeddings_secret ? {
    name = local.embeddings_secret_name
    key  = "apiKey"
  } : null
}
