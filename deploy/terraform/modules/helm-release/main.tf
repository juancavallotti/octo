# The octo Helm release. Cloud-agnostic: the kubernetes/helm providers are
# configured by the calling root (the k3s VM via a fetched kubeconfig, GKE/EKS via
# an in-memory token), and every cluster-specific decision — chart source, image
# registry, ingress class, TLS mode, bundled vs managed Postgres — arrives as a
# variable rather than being baked in here.
#
# Note that Helm applies --set after every values file, so anything this module
# sets explicitly wins over values_files and extra_values. Values a caller needs to
# own must therefore be ones this module leaves alone (or ones whose variable it
# can set to the empty "emit nothing" sentinel).

resource "helm_release" "octo" {
  name             = var.release_name
  namespace        = var.namespace
  create_namespace = var.create_namespace

  # An empty repository means `chart` is a local filesystem path. Credentials are
  # likewise optional: the helm provider only performs a registry login when both a
  # username and a password are present, so a public ghcr.io chart needs neither.
  repository          = var.repository != "" ? var.repository : null
  repository_username = var.registry_username != "" ? var.registry_username : null
  repository_password = var.registry_password != "" ? var.registry_password : null
  chart               = var.chart
  # A local chart is whatever version its Chart.yaml says; pinning one here would
  # just be a mismatch waiting to happen.
  version = var.repository != "" ? var.chart_version : null

  wait    = true
  atomic  = var.atomic
  timeout = var.timeout

  # Values files, layered under the `set` blocks below.
  #
  # values_files carries an environment profile shipped with the chart
  # (helm/values-*.yaml). extra_values carries raw YAML from the caller — the
  # escape hatch for maps with dotted keys that --set cannot express readably.
  # image_values_file comes last and therefore wins: it is rendered by
  # cloudbuild.yaml (or `task images:ttl`) and pins every image by the digest that
  # build pushed, because a tag can be moved or rebuilt and a digest cannot.
  values = concat(
    [for f in var.values_files : file(f)],
    var.extra_values,
    var.image_values_file != "" ? [file(var.image_values_file)] : [],
  )

  # --- Images ---
  # Per-component repository names are left at the chart defaults (the "-paas"
  # names, #199); only the registry needs overriding, and only when it is not the
  # chart's own public default.
  dynamic "set" {
    for_each = var.image_registry != "" ? toset(["image.registry"]) : toset([])
    content {
      name  = set.value
      value = var.image_registry
    }
  }

  dynamic "set" {
    for_each = var.image_pull_policy != "" ? toset(["image.pullPolicy"]) : toset([])
    content {
      name  = set.value
      value = var.image_pull_policy
    }
  }

  # Identify the images by tag only when digests were not supplied. Leaving this
  # in alongside a digest file would not actually break the pin — the chart
  # prefers image.digest over any tag — but it would leave two disagreeing
  # sources of truth for what the release runs.
  dynamic "set" {
    for_each = var.image_values_file == "" && var.image_tag != "" ? toset(["image.tag"]) : toset([])
    content {
      name  = set.value
      value = var.image_tag
    }
  }

  # --- Database ---
  # Bundled Postgres (chart creates the Secret + StatefulSet). Skipped entirely on
  # an external-database install, where there is no chart-owned password to set.
  dynamic "set_sensitive" {
    for_each = var.postgres_password != "" ? toset(["postgres.auth.password"]) : toset([])
    content {
      name  = set_sensitive.value
      value = var.postgres_password
    }
  }

  # Pin the database to a fixed node path instead of a dynamically provisioned
  # volume. Single-node clusters only — see the variable's description.
  dynamic "set" {
    for_each = var.postgres_host_path != "" ? toset(["postgres.storage.hostPath"]) : toset([])
    content {
      name  = set.value
      value = var.postgres_host_path
    }
  }

  # A managed instance instead. postgres.enabled=false drops the StatefulSet, its
  # Service and its Secret; the schema Job still runs, against this host. The
  # password is never a Helm value — it lives in a Secret the caller owns, because
  # Helm keeps every value it is given in release history.
  dynamic "set" {
    for_each = var.external_database == null ? {} : {
      "postgres.enabled"                           = "false"
      "externalDatabase.host"                      = var.external_database.host
      "externalDatabase.port"                      = tostring(var.external_database.port)
      "externalDatabase.user"                      = var.external_database.user
      "externalDatabase.database"                  = var.external_database.database
      "externalDatabase.sslmode"                   = var.external_database.sslmode
      "externalDatabase.existingSecret"            = var.external_database.existing_secret
      "externalDatabase.existingSecretPasswordKey" = var.external_database.existing_secret_password_key
    }
    content {
      name  = set.key
      value = set.value
    }
  }

  # --- Editor ingress + TLS ---
  set {
    name  = "ingress.enabled"
    value = var.ingress_enabled
  }
  set {
    name  = "ingress.host"
    value = var.domain
  }

  # Skipped where there is no cert-manager: on EKS the ALB terminates TLS against
  # an ACM certificate, and an empty clusterIssuer would make the chart's auto
  # mode select cert-manager anyway.
  dynamic "set" {
    for_each = var.cluster_issuer != "" ? {
      "ingress.tls.clusterIssuer"  = var.cluster_issuer
      "orchestrator.clusterIssuer" = var.cluster_issuer
    } : {}
    content {
      name  = set.key
      value = set.value
    }
  }

  dynamic "set" {
    for_each = var.tls_mode != "" ? toset(["ingress.tls.mode"]) : toset([])
    content {
      name  = set.value
      value = var.tls_mode
    }
  }

  dynamic "set" {
    for_each = var.certificate_arn != "" ? toset(["ingress.tls.certificateArn"]) : toset([])
    content {
      name  = set.value
      value = var.certificate_arn
    }
  }

  # Per-integration external endpoints (Stage 2) live under *.{apps_domain}, which
  # may be a different (Cloud-DNS-delegated) domain than the editor's own host.
  set {
    name  = "orchestrator.baseDomain"
    value = var.apps_domain
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

  # --- OIDC SSO ---
  # When enabled the chart creates the auth Secret and the editor mounts
  # AUTH_EETR_* / AUTH_SECRET. Sensitive values go through set_sensitive so they
  # are not printed in plans/logs.
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
