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

**No credential is passed through this module.** Every one arrives as the name of
a Secret the caller created — `modules/octo-secrets` creates them — because Helm
keeps every value it is given in the release history, where a credential outlives
the rotation meant to end it. There is no `set_sensitive` block in `main.tf`, and
adding one is the mistake to avoid.

Note that Helm applies `--set` after every values file, so anything this module
sets explicitly beats `values_files` and `extra_values`. Values a caller needs to
own must be ones the module leaves alone, or ones whose variable can be set to the
empty "emit nothing" sentinel.

**Used by:** infra, release, octo-gke, eks

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
