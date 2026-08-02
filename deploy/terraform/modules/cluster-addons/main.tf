# The cluster prerequisites the octo chart documents but does not install: an
# ingress controller, cert-manager, and the ClusterIssuers the chart's values name.
# On the k3s VM the startup script does this; a managed cluster has no startup
# script, so Terraform does it here.
#
# Ordering is the whole story, and it is expressed as an explicit chain —
# nginx -> cert-manager -> issuers -> (the caller's octo release). Terraform then
# gets the reverse order for free on destroy, which matters: tearing down the
# controller before the Service it owns would strand a forwarding rule.

locals {
  cert_manager_namespace = "cert-manager"
  # cert-manager's chart names its ServiceAccount after the release.
  cert_manager_ksa = "cert-manager"
}

# --- Workload Identity for cert-manager's DNS-01 solver ---
#
# The wildcard certificate can only be issued over DNS-01, which means
# cert-manager needs to write TXT records into Cloud DNS. The k3s VM solves this
# with ambient credentials (the node's service account); on GKE that is not an
# option — Autopilot will not give a pod the node identity at all — so the
# ServiceAccount is bound to a Google service account instead.

resource "google_service_account" "cert_manager" {
  count        = var.enable_dns01 ? 1 : 0
  project      = var.project_id
  account_id   = "${var.name}-certmgr"
  display_name = "cert-manager DNS-01 solver (${var.name})"
}

resource "google_project_iam_member" "cert_manager_dns" {
  count   = var.enable_dns01 ? 1 : 0
  project = var.project_id
  role    = "roles/dns.admin"
  member  = "serviceAccount:${google_service_account.cert_manager[0].email}"
}

resource "google_service_account_iam_member" "cert_manager_wi" {
  count              = var.enable_dns01 ? 1 : 0
  service_account_id = google_service_account.cert_manager[0].name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[${local.cert_manager_namespace}/${local.cert_manager_ksa}]"
}

# --- Ingress controller ---
#
# nginx rather than GCE ingress, for the reason values-gke-autopilot.yaml gives:
# the orchestrator creates an Ingress per externally-exposed integration at
# runtime, and GCE ingress needs a cloud.google.com/neg annotation on each backend
# Service that the orchestrator does not set.
resource "helm_release" "ingress_nginx" {
  name             = "ingress-nginx"
  repository       = "https://kubernetes.github.io/ingress-nginx"
  chart            = "ingress-nginx"
  version          = var.ingress_nginx_version
  namespace        = "ingress-nginx"
  create_namespace = true

  wait    = true
  timeout = var.timeout

  # The address reserved by modules/gke-cluster, so the DNS records already point
  # here by the time the controller comes up.
  set {
    name  = "controller.service.loadBalancerIP"
    value = var.load_balancer_ip
  }

  # Preserves the client source IP. Also means only nodes running a controller pod
  # pass the load balancer health check, which is fine for a Deployment.
  set {
    name  = "controller.service.externalTrafficPolicy"
    value = "Local"
  }

  # A Deployment, not the host-network DaemonSet some setups use: Autopilot
  # rejects the privileges that needs, and the chart's default already binds
  # 8080/8443 as an unprivileged uid behind a LoadBalancer Service.
  set {
    name  = "controller.kind"
    value = "Deployment"
  }

  # Autopilot bills on requests and injects defaults when they are absent.
  set {
    name  = "controller.resources.requests.cpu"
    value = "100m"
  }
  set {
    name  = "controller.resources.requests.memory"
    value = "256Mi"
  }

  dynamic "set" {
    for_each = var.ingress_nginx_extra_set
    content {
      name  = set.key
      value = set.value
    }
  }
}

# --- cert-manager ---
resource "helm_release" "cert_manager" {
  name             = "cert-manager"
  repository       = "https://charts.jetstack.io"
  chart            = "cert-manager"
  version          = var.cert_manager_version
  namespace        = local.cert_manager_namespace
  create_namespace = true

  wait    = true
  timeout = var.timeout

  set {
    name  = "crds.enabled"
    value = "true"
  }

  # GKE Autopilot denies writes to kube-system, where cert-manager takes its
  # leader-election Lease by default — the controller then crash-loops with a bare
  # "forbidden" that points nowhere near the actual cause. Harmless on Standard,
  # so it is set unconditionally rather than gated on the flavour.
  set {
    name  = "global.leaderElection.namespace"
    value = local.cert_manager_namespace
  }

  # Lets a ClusterIssuer's DNS-01 solver use the identity bound below instead of
  # requiring a key Secret in every namespace.
  set {
    name  = "extraArgs[0]"
    value = "--issuer-ambient-credentials=true"
  }

  dynamic "set" {
    for_each = var.enable_dns01 ? toset(["serviceAccount.annotations.iam\\.gke\\.io/gcp-service-account"]) : toset([])
    content {
      name  = set.value
      value = google_service_account.cert_manager[0].email
    }
  }

  depends_on = [
    helm_release.ingress_nginx,
    google_service_account_iam_member.cert_manager_wi,
  ]
}

# --- ClusterIssuers ---
#
# A local chart rather than kubernetes_manifest: that resource validates against
# the API schema at PLAN time, so it cannot create a custom resource whose CRD is
# installed by the same apply. See deploy/terraform/charts/README.md.
resource "helm_release" "cluster_issuers" {
  name      = "cluster-issuers"
  chart     = "${path.module}/../../charts/cluster-issuers"
  namespace = local.cert_manager_namespace

  wait    = true
  timeout = 300

  values = [yamlencode({
    acmeEmail = var.acme_email
    server    = var.acme_server
    http01 = {
      name         = var.cluster_issuer_name
      ingressClass = "nginx"
    }
    dns01 = {
      enabled = var.enable_dns01
      name    = var.dns01_cluster_issuer_name
      project = var.project_id
    }
  })]

  # The CRDs must exist and the webhook must be serving before a ClusterIssuer can
  # be admitted. cert-manager's wait = true covers the pods; the webhook can still
  # need a moment beyond that, which atomic + a retry-friendly timeout absorbs.
  depends_on = [helm_release.cert_manager]
}
