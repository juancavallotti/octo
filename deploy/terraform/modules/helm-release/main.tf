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

  # Values files, layered under the `set` blocks below (Helm applies --set last,
  # and so does this provider).
  #
  # values_files carries an environment profile shipped with the chart
  # (helm/values-*.yaml) for callers that want one. image_values_file is
  # rendered by cloudbuild.yaml and pins every image by the digest that build
  # pushed: a tag can be moved or rebuilt, a digest cannot, so the release
  # installs exactly what the build produced rather than whatever the tag happens
  # to point at by the time Helm resolves it.
  values = concat(
    [for f in var.values_files : file(f)],
    var.image_values_file != "" ? [file(var.image_values_file)] : [],
  )

  # Image coordinates. Per-component repository names are left at the chart
  # defaults (the "-paas" names, #199) — cloudbuild.yaml pushes Artifact Registry
  # images under those same names, so only the registry needs overriding here.
  set {
    name  = "image.registry"
    value = var.image_base
  }
  set {
    name  = "image.pullPolicy"
    value = "IfNotPresent"
  }
  # Identify the images by tag only when digests were not supplied. Leaving this
  # in alongside a digest file would not actually break the pin — the chart
  # prefers image.digest over any tag — but it would leave two disagreeing
  # sources of truth for what the release runs.
  dynamic "set" {
    for_each = var.image_values_file == "" ? toset(["image.tag"]) : toset([])
    content {
      name  = set.value
      value = var.image_tag
    }
  }

  # Postgres credentials (chart creates the Secret + StatefulSet).
  set_sensitive {
    name  = "postgres.auth.password"
    value = var.postgres_password
  }

  # Pin the database to a fixed node path instead of a dynamically provisioned
  # volume. Off by default because switching an existing release is a data move,
  # not a config change — see the variable's description.
  dynamic "set" {
    for_each = var.postgres_host_path != "" ? toset(["postgres.storage.hostPath"]) : toset([])
    content {
      name  = set.value
      value = var.postgres_host_path
    }
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

  # Per-integration external endpoints (Stage 2) live under *.{apps_domain}, which
  # may be a different (Cloud-DNS-delegated) domain than the editor's own host.
  set {
    name  = "orchestrator.baseDomain"
    value = var.apps_domain
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

  # for_each iterates over the (non-sensitive) Helm key names; the sensitive values
  # are looked up inside content so the collection itself isn't sensitive — Terraform
  # rejects sensitive values as for_each arguments.
  dynamic "set_sensitive" {
    for_each = var.oidc_enabled ? toset(["auth.oidc.clientSecret", "auth.secret"]) : toset([])
    content {
      name = set_sensitive.value
      value = {
        "auth.oidc.clientSecret" = var.oidc_client_secret
        "auth.secret"            = var.auth_secret
      }[set_sensitive.value]
    }
  }

  # KV secret-namespace encryption key. Supplied only when set so a key-less install
  # leaves encryption disabled (plain KV still works). set_sensitive keeps it out of
  # plans/logs.
  dynamic "set_sensitive" {
    for_each = nonsensitive(var.kv_encryption_key != "") ? toset(["kv.encryptionKey"]) : toset([])
    content {
      name  = set_sensitive.value
      value = var.kv_encryption_key
    }
  }
}
