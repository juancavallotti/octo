# The EKS prerequisites the octo chart documents but does not install: the AWS Load
# Balancer Controller (which provides the `alb` IngressClass) and the gp3
# StorageClass that helm/values-eks.yaml asks for.
#
# The EBS CSI *driver* is not here — it belongs in the cluster's `cluster_addons`,
# where EKS manages it. This module only creates the StorageClass on top of it.
#
# Note there is no cert-manager and no ClusterIssuer on this path: TLS terminates
# at the ALB against an ACM certificate, which is what ingress.tls.mode = acm
# selects in the chart.

data "aws_region" "current" {}

# IRSA. The controller calls the EC2 and ELB APIs to create load balancers, target
# groups and security groups, so it needs real IAM — and a pod gets that through a
# role assumed via the cluster's OIDC provider, not through node credentials.
#
# attach_load_balancer_controller_policy is the canned ~180-line AWS-published
# policy. Hand-writing it is the single most error-prone part of an EKS bootstrap:
# a missing action shows up as an Ingress that is simply never reconciled.
module "lb_controller_irsa" {
  source  = "terraform-aws-modules/iam/aws//modules/iam-role-for-service-accounts-eks"
  version = "~> 5.44"

  role_name                              = "${var.cluster_name}-alb-controller"
  attach_load_balancer_controller_policy = true

  oidc_providers = {
    main = {
      provider_arn               = var.oidc_provider_arn
      namespace_service_accounts = ["kube-system:aws-load-balancer-controller"]
    }
  }

  tags = var.tags
}

resource "helm_release" "aws_lb_controller" {
  name       = "aws-load-balancer-controller"
  repository = "https://aws.github.io/eks-charts"
  chart      = "aws-load-balancer-controller"
  version    = var.lb_controller_version
  namespace  = "kube-system"

  wait    = true
  timeout = var.timeout

  set {
    name  = "clusterName"
    value = var.cluster_name
  }
  set {
    name  = "region"
    value = data.aws_region.current.name
  }
  set {
    name  = "vpcId"
    value = var.vpc_id
  }
  set {
    name  = "serviceAccount.create"
    value = "true"
  }
  set {
    name  = "serviceAccount.name"
    value = "aws-load-balancer-controller"
  }
  # Dotted annotation key: Helm's --set needs the dots escaped, and HCL needs the
  # backslashes escaped again, hence the doubling.
  set {
    name  = "serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn"
    value = module.lb_controller_irsa.iam_role_arn
  }
}

# EKS ships a gp2 StorageClass and nothing else; helm/values-eks.yaml asks for gp3,
# which is both cheaper and faster. Provisioned by the EBS CSI driver the cluster
# installs as a managed addon.
resource "kubernetes_storage_class_v1" "gp3" {
  metadata {
    name = "gp3"
    annotations = {
      "storageclass.kubernetes.io/is-default-class" = var.gp3_is_default ? "true" : "false"
    }
  }

  storage_provisioner = "ebs.csi.aws.com"
  # EBS volumes are zonal. Binding on first consumer is what stops a volume being
  # provisioned in an availability zone the pod cannot actually be scheduled into
  # — with Immediate binding that is a Pending pod and a volume you still pay for.
  volume_binding_mode    = "WaitForFirstConsumer"
  allow_volume_expansion = true
  reclaim_policy         = "Retain"

  parameters = {
    type      = "gp3"
    encrypted = "true"
  }
}
