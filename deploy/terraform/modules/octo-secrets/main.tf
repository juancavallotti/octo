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
  namespace = var.namespace

  # Names computed here rather than read back off the resources, so every output is
  # known during plan — a caller feeds these into `for_each`/`count` on the Helm
  # side, and an unknown there is the same "Invalid count argument" the flags above
  # exist to avoid. Ordering is therefore NOT implied by the outputs: callers depend
  # on this module explicitly, because a Secret has to exist before the chart that
  # reads it installs.
  postgres_secret_name   = "${var.name_prefix}-db-password"
  auth_secret_name       = "${var.name_prefix}-auth-creds"
  kv_secret_name         = "${var.name_prefix}-kv-key"
  dev_runs_secret_name   = "${var.name_prefix}-devrun-key"
  embeddings_secret_name = "${var.name_prefix}-embeddings-key"
}

# The bundled Postgres password. The chart's own Secret still carries the username
# and database name (they are not credentials); only the password comes from here,
# which is exactly the split postgres.auth.existingSecret was built for.
resource "kubernetes_secret_v1" "postgres" {
  count = var.create_postgres_secret ? 1 : 0

  metadata {
    name      = local.postgres_secret_name
    namespace = local.namespace
  }

  data = {
    postgres-password = var.postgres_password
  }

  lifecycle {
    precondition {
      condition     = var.postgres_password != ""
      error_message = "create_postgres_secret is true but postgres_password is empty. The chart would read a Secret whose password key is the empty string, and every pod would fail authentication against the database."
    }
  }
}

# The editor's two authentication credentials in one Secret, because the chart
# reads them from one reference and they are turned on and off together.
resource "kubernetes_secret_v1" "auth" {
  count = var.create_auth_secret ? 1 : 0

  metadata {
    name      = local.auth_secret_name
    namespace = local.namespace
  }

  data = {
    "auth-secret"        = var.auth_secret
    "oidc-client-secret" = var.oidc_client_secret
  }

  lifecycle {
    precondition {
      condition     = var.auth_secret != "" && var.oidc_client_secret != ""
      error_message = "create_auth_secret is true but auth_secret or oidc_client_secret is empty. Both are required: the chart renders an SSO install from this one Secret, and a missing key fails at the first sign-in rather than at apply."
    }
  }
}

# The KV encryption key. Held apart from the rest deliberately: it is the one
# credential whose loss is not recoverable by rotation — a new key makes every
# value already written to a secret KV namespace unreadable — so it is worth being
# able to see, back up and restore it on its own.
resource "kubernetes_secret_v1" "kv" {
  count = var.create_kv_secret ? 1 : 0

  metadata {
    name      = local.kv_secret_name
    namespace = local.namespace
  }

  data = {
    kv-encryption-key = var.kv_encryption_key
  }

  lifecycle {
    precondition {
      condition     = var.kv_encryption_key != ""
      error_message = "create_kv_secret is true but kv_encryption_key is empty. Leave the flag false to run without KV encryption; an empty key rejects every secret-namespace write while looking configured."
    }
  }
}

# The dev-run hostname/identity HMAC key, apart for the same reason: rotating it
# re-labels every exposed dev run, and a webhook registered with Stripe or GitHub
# against the old hostname stops being delivered with nothing reporting it.
resource "kubernetes_secret_v1" "dev_runs" {
  count = var.create_dev_runs_secret ? 1 : 0

  metadata {
    name      = local.dev_runs_secret_name
    namespace = local.namespace
  }

  data = {
    dev-run-hash-secret = var.dev_run_hash_secret
  }

  lifecycle {
    precondition {
      condition     = var.dev_run_hash_secret != ""
      error_message = "create_dev_runs_secret is true but dev_run_hash_secret is empty. It keys the identity and public hostname of every dev run; an empty value is not a key."
    }
  }
}

# The embedding server's provider key — the one credential here that belongs to
# somebody else, and the one an operator will want to replace without touching
# anything around it.
resource "kubernetes_secret_v1" "embeddings" {
  count = var.create_embeddings_secret ? 1 : 0

  metadata {
    name      = local.embeddings_secret_name
    namespace = local.namespace
  }

  data = {
    apiKey = var.embeddings_api_key
  }

  lifecycle {
    precondition {
      condition     = var.embeddings_api_key != ""
      error_message = "create_embeddings_secret is true but embeddings_api_key is empty. Leave the flag false to run without an embedding server."
    }
  }
}
