# `eks-addons`

EKS prerequisites: AWS Load Balancer Controller, gp3 StorageClass.

The controller that provides the `alb` IngressClass, with the IRSA role it needs,
plus the `gp3` StorageClass `helm/values-eks.yaml` asks for (EKS ships only `gp2`).

The EBS CSI *driver* is not here — it belongs in the cluster's `cluster_addons`,
where EKS manages it. This module only creates the StorageClass on top of it.

There is no cert-manager on this path: TLS terminates at the ALB against an ACM
certificate, which is what `ingress.tls.mode = acm` selects in the chart.

**Used by:** eks

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
