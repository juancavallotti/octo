# `gke-cluster`

A GKE cluster and its network.

Autopilot or Standard, selected by `autopilot`. Creates a dedicated VPC rather
than reusing `default`, where the k3s VM lives — a test cluster that gets destroyed
and recreated has no business sharing blast radius with it.

Also creates the **reserved ingress IP**, which is what lets DNS records be written
in the same apply that creates the cluster, and optionally the Private Services
Access peering a Cloud SQL private IP needs.

The cluster is VPC-native in both flavours. That is load-bearing, not incidental:
pod IPs come from a subnet secondary range, subnet routes are exported over the
service-networking peering, and that is precisely why a pod can reach a Cloud SQL
private IP with no proxy sidecar.

**Used by:** octo-gke

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
