# Every credential the octo chart needs, installed into the cluster as Secrets
# before the chart is applied — so the chart reads them by reference and no
# credential is ever passed to Helm as a value.
#
# That distinction is the whole reason this module exists. Helm keeps every value
# it is given in the release history: a `--set` (or `set_sensitive`, which only
# affects Terraform's own plan output) lands in the release's Secret in the
# cluster, and stays there for every retained revision — long after the credential
# was rotated, and readable by anyone who can read Secrets in the namespace. A
# Secret created here is one object, replaced in place when the value changes, and
# gone when it is destroyed.
#
# The chart already accepted a reference for every credential; the roots simply
# were not giving it one. See modules/helm-release, which now takes only names and
# keys.
#
# Key names match the chart's own defaults throughout, so the corresponding
# `existingSecretKey` values never have to be passed at all. They are still
# emitted as outputs rather than assumed on the far side — a default that two
# modules agree on by coincidence is a default that drifts.
#
# Secret NAMES deliberately do not match the chart's. The chart names its own
# Secrets `{release}-{component}` — `octo-postgres`, `octo-auth`, `octo-kv`,
# `octo-devruns`, `octo-embeddings` — and a Secret created here under one of
# those names breaks in two different ways. `{release}-postgres` collides
# outright, because the chart creates that one whatever the password's source: it
# also carries the username and database name, so Helm refuses to install over a
# Secret it does not own. The other four collide only on the way in: an
# installation migrating from an inline value to a reference has a chart-owned
# Secret of that name in the previous revision, so the upgrade that stops
# rendering it DELETES the very Secret the new revision points at.
#
# Hence a content-describing suffix on each. `helm template` cannot detect either
# failure — ownership is a property of the release, not of the rendered manifest —
# so the naming is the only thing preventing them.

resource "kubernetes_namespace_v1" "octo" {
  count = var.create_namespace ? 1 : 0

  metadata {
    name = var.namespace
  }
}

locals {
  namespace = var.create_namespace ? kubernetes_namespace_v1.octo[0].metadata[0].name : var.namespace

  # nonsensitive() on the predicates: count rejects a value derived from a
  # sensitive one, and whether a credential was supplied is not itself a secret.
  postgres   = nonsensitive(var.postgres_password != "")
  auth       = nonsensitive(var.auth_secret != "" || var.oidc_client_secret != "")
  kv         = nonsensitive(var.kv_encryption_key != "")
  dev_runs   = nonsensitive(var.dev_run_hash_secret != "")
  embeddings = nonsensitive(var.embeddings_api_key != "")
}

# The bundled Postgres password. The chart's own Secret still carries the username
# and database name (they are not credentials); only the password comes from here,
# which is exactly the split postgres.auth.existingSecret was built for.
resource "kubernetes_secret_v1" "postgres" {
  count = local.postgres ? 1 : 0

  metadata {
    name      = "${var.name_prefix}-db-password"
    namespace = local.namespace
  }

  data = {
    postgres-password = var.postgres_password
  }
}

# The editor's two authentication credentials in one Secret, because the chart
# reads them from one reference and they are turned on and off together.
resource "kubernetes_secret_v1" "auth" {
  count = local.auth ? 1 : 0

  metadata {
    name      = "${var.name_prefix}-auth-creds"
    namespace = local.namespace
  }

  data = {
    "auth-secret"        = var.auth_secret
    "oidc-client-secret" = var.oidc_client_secret
  }
}

# The KV encryption key. Held apart from the rest deliberately: it is the one
# credential whose loss is not recoverable by rotation — a new key makes every
# value already written to a secret KV namespace unreadable — so it is worth being
# able to see, back up and restore it on its own.
resource "kubernetes_secret_v1" "kv" {
  count = local.kv ? 1 : 0

  metadata {
    name      = "${var.name_prefix}-kv-key"
    namespace = local.namespace
  }

  data = {
    kv-encryption-key = var.kv_encryption_key
  }
}

# The dev-run hostname/identity HMAC key, apart for the same reason: rotating it
# re-labels every exposed dev run, and a webhook registered with Stripe or GitHub
# against the old hostname stops being delivered with nothing reporting it.
resource "kubernetes_secret_v1" "dev_runs" {
  count = local.dev_runs ? 1 : 0

  metadata {
    name      = "${var.name_prefix}-devrun-key"
    namespace = local.namespace
  }

  data = {
    dev-run-hash-secret = var.dev_run_hash_secret
  }
}

# The embedding server's provider key — the one credential here that belongs to
# somebody else, and the one an operator will want to replace without touching
# anything around it.
resource "kubernetes_secret_v1" "embeddings" {
  count = local.embeddings ? 1 : 0

  metadata {
    name      = "${var.name_prefix}-embeddings-key"
    namespace = local.namespace
  }

  data = {
    apiKey = var.embeddings_api_key
  }
}
