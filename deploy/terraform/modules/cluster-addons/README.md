# `cluster-addons`

GKE prerequisites: ingress-nginx, cert-manager, ClusterIssuers.

The controllers the octo chart documents but does not install. On the k3s VM the
startup script does this; a managed cluster has no startup script.

Includes the Workload Identity plumbing for cert-manager's DNS-01 solver — a
Google service account with `roles/dns.admin`, bound to the `cert-manager`
Kubernetes ServiceAccount. Ambient node credentials (what the k3s VM uses) are not
an option on GKE, least of all Autopilot.

The ClusterIssuers come from a small local chart rather than `kubernetes_manifest`,
which validates against the API schema at *plan* time and therefore cannot create a
custom resource whose CRD is installed by the same apply. See `../../charts/`.

**Used by:** octo-gke

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
