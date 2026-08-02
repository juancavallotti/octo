# Terraform-installed charts

Helm charts that exist to bootstrap a cluster, not to ship octo. They are installed
by the Terraform modules in `../modules` and are **not** published anywhere.

This repo publishes exactly two charts — `octo` and the `octo-common` library, both
under `helm/` — and that stays true. Every packaging path (`task helm:package`,
`cloudbuild.yaml`, `.github/workflows/release.yml`, `release-please-config.json`)
names those two by explicit path, so nothing here is picked up by a release.

| Chart | Installed by | Why it is a chart |
|---|---|---|
| `cluster-issuers` | `modules/cluster-addons` | cert-manager `ClusterIssuer`s are custom resources. Terraform's `kubernetes_manifest` validates against the API schema at **plan** time, so it cannot create a CR whose CRD is installed by the same apply — the standard cert-manager bootstrap problem. A `helm_release` applies at apply time and needs no extra provider. |
