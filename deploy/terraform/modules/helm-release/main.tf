# The octo Helm release, installed from the Artifact Registry OCI chart. The
# kubernetes/helm providers are configured by the calling root (pointed at the
# k3s API). Image pulls happen on the node, so the caller must ensure the images
# for image_tag are present (the release root invokes octo-pull before this).

resource "helm_release" "octo" {
  name             = var.release_name
  namespace        = var.namespace
  create_namespace = true

  # OCI chart in Artifact Registry: oci://{image_base} + chart name "octo".
  repository          = "oci://${var.image_base}"
  repository_username = var.registry_username
  repository_password = var.registry_password
  chart               = "octo"
  version             = var.chart_version

  wait    = true
  timeout = var.timeout

  # Image coordinates.
  set {
    name  = "image.registry"
    value = var.image_base
  }
  set {
    name  = "image.tag"
    value = var.image_tag
  }
  set {
    name  = "image.pullPolicy"
    value = "IfNotPresent"
  }

  # Postgres credentials (chart creates the Secret + StatefulSet).
  set_sensitive {
    name  = "postgres.auth.password"
    value = var.postgres_password
  }

  # Editor ingress + TLS.
  set {
    name  = "ingress.enabled"
    value = "true"
  }
  set {
    name  = "ingress.host"
    value = var.domain
  }
  set {
    name  = "ingress.tls.clusterIssuer"
    value = var.cluster_issuer
  }

  # Per-integration external endpoints (Stage 2) live under *.{domain}.
  set {
    name  = "orchestrator.baseDomain"
    value = var.domain
  }
  set {
    name  = "orchestrator.clusterIssuer"
    value = var.cluster_issuer
  }

  # Shared wildcard cert (DNS-01) so per-integration subdomains validate without a
  # cert per subdomain. When on, the editor + per-integration ingresses reference
  # the one wildcard Secret instead of issuing per-host certs via cluster_issuer.
  set {
    name  = "wildcardTLS.enabled"
    value = var.wildcard_tls
  }
  set {
    name  = "wildcardTLS.clusterIssuer"
    value = var.wildcard_cluster_issuer
  }

  # OIDC SSO for the editor. When enabled the chart creates the auth Secret and the
  # editor mounts AUTH_EETR_* / AUTH_SECRET. Sensitive values go through
  # set_sensitive so they are not printed in plans/logs.
  set {
    name  = "auth.oidc.enabled"
    value = var.oidc_enabled
  }

  dynamic "set" {
    for_each = var.oidc_enabled ? merge(
      {
        "auth.oidc.issuer"   = var.oidc_issuer
        "auth.oidc.clientId" = var.oidc_client_id
      },
      var.oidc_write_roles != "" ? { "auth.writeRoles" = var.oidc_write_roles } : {},
      var.oidc_roles_claim != "" ? { "auth.rolesClaim" = var.oidc_roles_claim } : {},
    ) : {}
    content {
      name  = set.key
      value = set.value
    }
  }

  dynamic "set_sensitive" {
    for_each = var.oidc_enabled ? {
      "auth.oidc.clientSecret" = var.oidc_client_secret
      "auth.secret"            = var.auth_secret
    } : {}
    content {
      name  = set_sensitive.key
      value = set_sensitive.value
    }
  }

  # KV secret-namespace encryption key. Supplied only when set so a key-less install
  # leaves encryption disabled (plain KV still works). set_sensitive keeps it out of
  # plans/logs.
  dynamic "set_sensitive" {
    for_each = var.kv_encryption_key != "" ? { "kv.encryptionKey" = var.kv_encryption_key } : {}
    content {
      name  = set_sensitive.key
      value = set_sensitive.value
    }
  }
}
