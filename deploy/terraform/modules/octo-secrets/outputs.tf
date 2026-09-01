# One output per credential, each a {name, key} reference or null when the
# credential was not supplied. Null rather than an empty object so a caller can
# hand it straight to modules/helm-release, whose reference variables read an
# empty name as "emit nothing" and leave the chart's values file to decide.

output "namespace" {
  description = "The namespace the Secrets were created in. Pass this to the helm-release module (with create_namespace = false) rather than the variable, so the release cannot be planned before the namespace exists."
  value       = local.namespace
}

output "postgres" {
  description = "Reference to the bundled-Postgres password Secret, or null on an external-database install."
  value = local.postgres ? {
    name = kubernetes_secret_v1.postgres[0].metadata[0].name
    key  = "postgres-password"
  } : null
}

output "auth" {
  description = "Reference to the editor's auth Secret, carrying both the Auth.js session secret and the OIDC client secret. Null when SSO is off."
  value = local.auth ? {
    name              = kubernetes_secret_v1.auth[0].metadata[0].name
    auth_secret_key   = "auth-secret"
    client_secret_key = "oidc-client-secret"
  } : null
}

output "kv" {
  description = "Reference to the KV encryption key Secret, or null when KV encryption is off."
  value = local.kv ? {
    name = kubernetes_secret_v1.kv[0].metadata[0].name
    key  = "kv-encryption-key"
  } : null
}

output "dev_runs" {
  description = "Reference to the dev-run HMAC key Secret, or null when no key was supplied — in which case the chart refuses to render while dev runs are enabled."
  value = local.dev_runs ? {
    name = kubernetes_secret_v1.dev_runs[0].metadata[0].name
    key  = "dev-run-hash-secret"
  } : null
}

output "embeddings" {
  description = "Reference to the embedding server's provider-key Secret, or null when there is no embedding server."
  value = local.embeddings ? {
    name = kubernetes_secret_v1.embeddings[0].metadata[0].name
    key  = "apiKey"
  } : null
}
