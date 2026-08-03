# `octo-gke`

octo on GKE, end to end.

The composition both GKE roots share: cluster, prerequisite controllers, DNS
records, an optional Cloud SQL instance, and the chart. The roots differ only in
the `autopilot` flag, their defaults, and which values profile is installed.

Keeping the composition in one place is what stops the two from quietly drifting,
which matters because the whole point of having both is that the *same* deployment
is exercised on each.

The kubernetes and helm providers are configured by the calling root (from an
access token, no kubeconfig on disk) and inherited here.

**Used by:** gke-standard, gke-autopilot

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
