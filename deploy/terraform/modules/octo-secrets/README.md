# `octo-secrets`

Every credential the octo chart needs, installed into the cluster as Kubernetes
Secrets **before** the chart is applied, so the chart reads them by reference and
no credential is ever passed to Helm as a value.

That is the whole point. Helm keeps every value it is given in the release
history — `set_sensitive` hides a value from Terraform's plan output, not from
the release Secret it is written into. A credential passed as a chart value is
therefore in the cluster for as long as the revision carrying it is retained,
which is long after it was rotated. A credential passed as a *reference* is one
object, replaced in place, and gone when it is destroyed.

| Secret | Key | Chart value it satisfies |
|---|---|---|
| `{prefix}-postgres` | `postgres-password` | `postgres.auth.existingSecret` |
| `{prefix}-auth` | `auth-secret`, `oidc-client-secret` | `auth.existingSecret` |
| `{prefix}-kv` | `kv-encryption-key` | `kv.existingSecret` |
| `{prefix}-devruns` | `dev-run-hash-secret` | `orchestrator.devRuns.existingSecret` |
| `{prefix}-embeddings` | `apiKey` | `embeddings.existingSecret` |

Every credential is optional; an empty value creates no Secret and the matching
output is `null`, which `modules/helm-release` reads as "emit nothing" so a
values profile keeps its say.

The module also creates the namespace by default, because a Secret has to exist
before the chart that references it installs and a namespace Helm has not created
yet is nowhere to put one. Pass its `namespace` output — and
`create_namespace = false` — to `modules/helm-release`.

A managed database's password is **not** here: it is issued by the cloud provider
rather than generated for this installation, so the root that creates the
instance creates its Secret alongside it and names it in
`helm-release`'s `external_database.existing_secret`.

**Used by:** release, octo-gke, eks

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
