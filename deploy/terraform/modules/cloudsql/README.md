# `cloudsql`

Cloud SQL for PostgreSQL on a private IP.

Instance, database, and an owner user with a generated password.

Private IP rather than a public IP with authorized networks: GKE node IPs are
ephemeral, so an allowlist would need rewriting every time the pool scales, and the
octo chart has no Cloud SQL Auth Proxy sidecar seam — a routable private address is
what lets the chart's DSN work unmodified.

**Do not add a least-privilege application user.** On PostgreSQL 16 the `CREATE`
privilege on schema `public` is revoked from `PUBLIC`, so a non-owner cannot create
tables — and the chart's schema Job does exactly that.

**Used by:** octo-gke

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
