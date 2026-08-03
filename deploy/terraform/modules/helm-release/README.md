# `helm-release`

The octo Helm release.

Cloud-agnostic. Every cluster-specific decision — chart source, image registry,
ingress class, TLS mode, bundled vs managed Postgres — arrives as a variable, and
the kubernetes/helm providers are configured by the calling root.

Three chart sources are supported by the same three variables:

| Source | `repository` | `chart` | `chart_version` |
|---|---|---|---|
| Artifact Registry OCI | `oci://REGION-docker.pkg.dev/PROJECT/octo` | `octo` | required, + credentials |
| public ghcr.io OCI | `oci://ghcr.io/juancavallotti/charts` | `octo` | required, no credentials |
| local working tree | `""` | `../../../helm` | `null` |

Note that Helm applies `--set` after every values file, so anything this module
sets explicitly beats `values_files` and `extra_values`. Values a caller needs to
own must be ones the module leaves alone, or ones whose variable can be set to the
empty "emit nothing" sentinel.

**Used by:** infra, release, octo-gke, eks

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
