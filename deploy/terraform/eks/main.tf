# octo on EKS: a VPC, the cluster, the AWS Load Balancer Controller, an ACM
# certificate, DNS and the chart — one apply, from nothing to a working editor at
# https://{domain}.
#
#   task eks:apply
#
# This exists to exercise helm/values-eks.yaml on a real cluster. It is a TEST
# environment and its defaults say so: spot nodes in public subnets, no NAT
# gateway, an RDS instance with no backups and no final snapshot. Each has a
# variable that turns it into the production choice.
#
# COST: the EKS control plane is ~$0.10/hour (~$73/month) and has no free tier. It
# starts billing the moment the cluster exists, whether or not anything runs on it.
# Destroy the root when you are done testing rather than leaving it idle. That rate
# holds only while kubernetes_version is in standard support; an extended-support
# version is $0.60/hour for the same cluster.
#
# The Route53 zone is NOT managed here. It is created once by hand and delegated
# from whoever holds the parent domain (see ../README.md), because Route53 assigns
# fresh nameservers on every zone creation — a zone inside a root you destroy and
# recreate would invalidate the delegation every cycle. Records ARE owned here.
#
# Tear down with `task eks:destroy`, which is phased on purpose: the Load Balancer
# Controller owns the ALB, and destroying it alongside the release strands the ALB
# and its security groups, which then block the VPC delete with an error that names
# a leftover ENI rather than the cause.

locals {
  repo_root = "${path.module}/../../.."

  # Values profiles are always read from this checkout: the module reads them with
  # file() at plan time, so they come from the working tree even when the chart
  # itself is pulled from ghcr.
  values_profile = "${local.repo_root}/helm/values-eks.yaml"

  chart_repository = var.chart_source == "oci" ? "oci://ghcr.io/juancavallotti/charts" : ""
  chart            = var.chart_source == "oci" ? "octo" : "${local.repo_root}/helm"

  apps_domain_eff = var.apps_domain != "" ? var.apps_domain : var.domain

  # Two AZs, not three. An ALB needs two; a third only multiplies subnets and nodes.
  azs = slice(data.aws_availability_zones.available.names, 0, 2)

  tags = merge(var.tags, {
    "octo-root" = "eks"
  })
}

data "aws_availability_zones" "available" {
  state = "available"
}

# The delegated zone, created by hand and shared with nothing else. Looked up
# rather than managed so `terraform destroy` cannot take the delegation with it.
data "aws_route53_zone" "this" {
  name         = var.route53_zone_name
  private_zone = false
}

# --- Network ---
#
# The upstream module rather than hand-written HCL. Not for the line count: the
# subnet tagging, route tables and NAT wiring here are exactly the parts whose
# subtle mistakes surface as an Ingress that never gets an address, or a destroy
# that hangs on a dangling ENI.
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.21.0"

  name = var.name
  cidr = var.vpc_cidr
  azs  = local.azs

  private_subnets = [for i in range(length(local.azs)) : cidrsubnet(var.vpc_cidr, 4, i)]
  public_subnets  = [for i in range(length(local.azs)) : cidrsubnet(var.vpc_cidr, 4, i + 8)]

  # A NAT gateway is ~$32/month plus data processing, purely so private nodes can
  # reach Docker Hub and the EKS API. Public nodes reach them directly. Turning
  # private_nodes on flips both this and where the node group is placed.
  enable_nat_gateway = var.private_nodes
  single_nat_gateway = var.private_nodes

  enable_dns_hostnames = true
  enable_dns_support   = true

  # How the AWS Load Balancer Controller discovers where to put an ALB. Without
  # these an internet-facing Ingress is simply never given an address, and the
  # controller logs a subnet-discovery error rather than anything about tags.
  public_subnet_tags  = { "kubernetes.io/role/elb" = "1" }
  private_subnet_tags = { "kubernetes.io/role/internal-elb" = "1" }

  tags = local.tags
}

# --- Cluster ---

module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "20.37.2"

  cluster_name    = var.name
  cluster_version = var.kubernetes_version

  vpc_id = module.vpc.vpc_id
  # Public subnets by default so the nodes need no NAT gateway.
  subnet_ids = var.private_nodes ? module.vpc.private_subnets : module.vpc.public_subnets

  cluster_endpoint_public_access = true

  # Module v20 uses EKS access entries rather than the aws-auth ConfigMap. Without
  # this the principal running `terraform apply` has no access to the cluster it
  # just created, and every kubernetes/helm resource fails with a 401 that reads
  # like a credentials problem rather than an authorization one.
  enable_cluster_creator_admin_permissions = true

  cluster_addons = {
    coredns    = {}
    kube-proxy = {}
    vpc-cni    = {}
    # Backs the gp3 StorageClass that modules/eks-addons creates and
    # helm/values-eks.yaml asks for. Needs its own IRSA role to call the EC2
    # volume APIs.
    aws-ebs-csi-driver = {
      service_account_role_arn = module.ebs_csi_irsa.iam_role_arn
    }
  }

  eks_managed_node_groups = {
    default = {
      # t3.medium is too tight once the bundled Postgres asks for 500m/2Gi
      # alongside kube-system, the ALB controller and the EBS CSI driver.
      instance_types = var.node_instance_types
      capacity_type  = var.spot_nodes ? "SPOT" : "ON_DEMAND"

      min_size     = var.node_min_size
      max_size     = var.node_max_size
      desired_size = var.node_desired_size

      # Nodes in public subnets need a public IP to reach the internet at all.
      subnet_ids = var.private_nodes ? module.vpc.private_subnets : module.vpc.public_subnets
    }
  }

  tags = local.tags
}

module "ebs_csi_irsa" {
  source  = "terraform-aws-modules/iam/aws//modules/iam-role-for-service-accounts-eks"
  version = "5.60.0"

  role_name             = "${var.name}-ebs-csi"
  attach_ebs_csi_policy = true

  oidc_providers = {
    main = {
      provider_arn               = module.eks.oidc_provider_arn
      namespace_service_accounts = ["kube-system:ebs-csi-controller-sa"]
    }
  }

  tags = local.tags
}

module "addons" {
  source = "../modules/eks-addons"

  cluster_name      = module.eks.cluster_name
  vpc_id            = module.vpc.vpc_id
  oidc_provider_arn = module.eks.oidc_provider_arn
  tags              = local.tags
}

# --- TLS ---
#
# ACM, not cert-manager: TLS terminates at the ALB, which is what
# ingress.tls.mode = acm selects in the chart. One certificate covers the editor
# host and every per-integration subdomain.

resource "aws_acm_certificate" "octo" {
  domain_name       = var.domain
  validation_method = "DNS"

  subject_alternative_names = distinct(compact([
    local.apps_domain_eff,
    "*.${local.apps_domain_eff}",
  ]))

  lifecycle {
    create_before_destroy = true
  }

  tags = local.tags
}

locals {
  # The names that need a validation record, derived from configuration rather
  # than from the certificate.
  #
  # The wildcard is deliberately absent. ACM validates *.apps.example.com with the
  # SAME CNAME as apps.example.com — one _<hash>.apps.example.com record covers
  # both — so including it would have two resource instances writing one record
  # and fighting over it.
  acm_validation_domains = distinct(compact([var.domain, local.apps_domain_eff]))
}

# Keys from configuration, values from the certificate. That split is not a style
# choice: for_each keys must be known at plan time, and every field of
# domain_validation_options — including the name and the domain it belongs to — is
# unknown until ACM has issued. Keying off any of them fails the plan outright with
# "keys derived from resource attributes that cannot be determined until apply".
resource "aws_route53_record" "acm_validation" {
  for_each = toset(local.acm_validation_domains)

  zone_id = data.aws_route53_zone.this.zone_id
  name    = one([for o in aws_acm_certificate.octo.domain_validation_options : o.resource_record_name if o.domain_name == each.key])
  type    = one([for o in aws_acm_certificate.octo.domain_validation_options : o.resource_record_type if o.domain_name == each.key])
  records = [one([for o in aws_acm_certificate.octo.domain_validation_options : o.resource_record_value if o.domain_name == each.key])]
  ttl     = 60

  # The zone is shared with whatever else lives under it, and a re-apply after a
  # certificate is replaced writes the same name again.
  allow_overwrite = true
}

# Blocks until ACM sees the records and issues. Five minutes of no visible progress
# here is normal, not a failure — the ALB cannot be created until it completes,
# because the Ingress annotation carries the ARN.
resource "aws_acm_certificate_validation" "octo" {
  certificate_arn         = aws_acm_certificate.octo.arn
  validation_record_fqdns = [for r in aws_route53_record.acm_validation : r.fqdn]
}

# --- Database ---

module "rds" {
  source = "../modules/rds"
  count  = var.external_database ? 1 : 0

  name   = "${var.name}-pg"
  vpc_id = module.vpc.vpc_id
  # Always the private subnets: the instance is never publicly accessible, and a
  # DB subnet group needs two AZs regardless.
  subnet_ids               = module.vpc.private_subnets
  client_security_group_id = module.eks.node_security_group_id
  instance_class           = var.rds_instance_class
  tags                     = local.tags
}

resource "random_password" "postgres" {
  count   = var.external_database ? 0 : 1
  length  = 24
  special = false
}

resource "kubernetes_namespace_v1" "octo" {
  metadata {
    name = var.namespace
  }
  depends_on = [module.addons]
}

# The managed database's password reaches the chart through a Secret, never through
# a Helm value: Helm stores every value it is given in the release history Secret,
# where it outlives any rotation. This is also the path helm/values-eks.yaml and the
# docs tell users to take, so it is the one worth exercising.
resource "kubernetes_secret_v1" "db" {
  count = var.external_database ? 1 : 0

  metadata {
    name      = "octo-rds"
    namespace = kubernetes_namespace_v1.octo.metadata[0].name
  }

  data = {
    password = module.rds[0].password
  }
}

# --- Application secrets ---
#
# Minted here rather than asked for. Neither has anything to stay consistent with
# across a destroy/recreate cycle — the database holding the sessions and the
# ciphertext goes with the cluster — and both are stable across applies because
# they live in this root's state.

# Auth.js session secret. Created unconditionally so that turning SSO on later is
# an in-place update rather than a new secret; only passed to the chart when SSO
# is actually enabled, which is the only case where the editor reads it.
resource "random_password" "auth_secret" {
  length  = 32
  special = false
}

# KV secret-namespace encryption key: 32 random bytes (AES-256), base64 for the
# chart. Not optional in practice — with kv.encryptionKey empty the chart rejects
# every write to a secret KV namespace (plain KV keeps working), so an integration
# that stores a credential fails on this cluster and nowhere else. That is a
# property of the deployment, not of the chart under test, so it is not something
# to leave to a tfvars line someone forgets.
#
# ignore_changes on length: regenerating it would orphan existing ciphertext.
resource "random_bytes" "kv_encryption_key" {
  length = 32

  lifecycle {
    ignore_changes = [length]
  }
}

# --- The release ---

module "octo" {
  source = "../modules/helm-release"

  release_name     = "octo"
  namespace        = kubernetes_namespace_v1.octo.metadata[0].name
  create_namespace = false

  repository    = local.chart_repository
  chart         = local.chart
  chart_version = var.chart_version

  image_registry    = ""
  image_tag         = ""
  image_pull_policy = ""
  image_values_file = var.image_values_file

  values_files = [local.values_profile]

  domain      = var.domain
  apps_domain = local.apps_domain_eff

  # No cert-manager on this path, so no ClusterIssuer and no wildcard Certificate —
  # the one ACM cert above already covers every host.
  cluster_issuer  = ""
  wildcard_tls    = false
  tls_mode        = "acm"
  certificate_arn = aws_acm_certificate_validation.octo.certificate_arn

  # The orchestrator stamps these onto every per-integration Ingress it creates at
  # runtime (INGRESS_ANNOTATIONS), so those share the editor's ALB and its
  # certificate rather than provisioning — and billing — one load balancer each.
  # Passed as YAML rather than --set because the keys contain dots, which --set
  # escaping renders unreadable.
  extra_values = [yamlencode({
    orchestrator = {
      ingressAnnotations = {
        "alb.ingress.kubernetes.io/scheme"          = "internet-facing"
        "alb.ingress.kubernetes.io/target-type"     = "ip"
        "alb.ingress.kubernetes.io/group.name"      = "octo"
        "alb.ingress.kubernetes.io/certificate-arn" = aws_acm_certificate_validation.octo.certificate_arn
      }
    }
  })]

  postgres_password = var.external_database ? "" : random_password.postgres[0].result
  external_database = var.external_database ? {
    host            = module.rds[0].host
    port            = module.rds[0].port
    user            = module.rds[0].database_user
    database        = module.rds[0].database_name
    sslmode         = "require"
    existing_secret = kubernetes_secret_v1.db[0].metadata[0].name
  } : null

  # --- Editor authentication ---
  oidc_enabled       = var.oidc_enabled
  oidc_issuer        = var.oidc_issuer
  oidc_client_id     = var.oidc_client_id
  oidc_client_secret = var.oidc_client_secret
  oidc_write_roles   = var.oidc_write_roles
  oidc_roles_claim   = var.oidc_roles_claim
  auth_secret        = var.oidc_enabled ? random_password.auth_secret.result : ""

  kv_encryption_key = random_bytes.kv_encryption_key.base64

  atomic  = true
  timeout = var.helm_timeout

  # Explicit, and load-bearing in both directions: on create the controller must
  # exist before the Ingress means anything; on destroy this ordering is what lets
  # the controller observe the Ingress being deleted and clean up the ALB.
  depends_on = [module.addons]
}

# --- DNS ---
#
# Unlike GKE, the ALB's address cannot be reserved ahead of time — the controller
# creates it in response to the Ingress. So it is read back afterwards.

# The Ingress exists as soon as Helm applies it; its status.loadBalancer is
# populated only once the controller has finished provisioning the ALB, which takes
# a couple of minutes. Reading too early yields an empty list and a confusing
# index-out-of-range at apply time.
resource "time_sleep" "alb_provisioned" {
  depends_on      = [module.octo]
  create_duration = var.alb_wait
}

data "kubernetes_ingress_v1" "octo" {
  metadata {
    name      = "octo-platform"
    namespace = kubernetes_namespace_v1.octo.metadata[0].name
  }
  depends_on = [time_sleep.alb_provisioned]
}

locals {
  alb_hostname = try(data.kubernetes_ingress_v1.octo.status[0].load_balancer[0].ingress[0].hostname, "")
}

# CNAME rather than an ALIAS: an ALIAS needs the ALB's hosted zone id, which would
# mean a second lookup for no benefit here. Neither name is a zone apex, so a CNAME
# is legal — which is also why var.domain must be a subdomain.
#
# Note what does NOT gate these: the ALB hostname. Guarding with
# `count = local.alb_hostname != "" ? 1 : 0` reads like prudence and is actually a
# plan-time error — count must be known before apply, and that value comes from an
# Ingress that does not exist yet. The emptiness case is handled where it belongs,
# as a precondition on the value, which also gets to say something useful about it.
resource "aws_route53_record" "editor" {
  zone_id         = data.aws_route53_zone.this.zone_id
  name            = var.domain
  type            = "CNAME"
  ttl             = 60
  records         = [local.alb_hostname]
  allow_overwrite = true

  lifecycle {
    precondition {
      condition     = local.alb_hostname != ""
      error_message = "The editor Ingress has no load-balancer hostname yet. The AWS Load Balancer Controller had not finished provisioning the ALB when it was read — raise alb_wait and apply again. Do not remove the wait."
    }
  }
}

resource "aws_route53_record" "wildcard" {
  # Config-derived, so this one is legitimate: it asks whether per-integration
  # endpoints were requested at all, which is knowable from the tfvars.
  count           = var.apps_domain != "" ? 1 : 0
  zone_id         = data.aws_route53_zone.this.zone_id
  name            = "*.${local.apps_domain_eff}"
  type            = "CNAME"
  ttl             = 60
  records         = [local.alb_hostname]
  allow_overwrite = true
}
