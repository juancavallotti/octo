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
#
# NO CREDENTIAL IS PASSED THROUGH HERE, and that is a property to preserve rather
# than a coincidence of the current variables. Helm keeps every value it is given
# in the release history — set_sensitive marks a value secret to Terraform's plan
# output, not to the release Secret Helm writes it into — so a credential passed
# as a chart value sits in the cluster for as long as the revision carrying it is
# retained, outliving the rotation that was supposed to end it. Every credential
# arrives instead as the name of a Secret the caller created, which
# modules/octo-secrets exists to create. There is no set_sensitive block below,
# and adding one is the mistake this note is here to prevent.

locals {
  # The embedding server needs both a decision and a key; without the Secret there
  # is nothing for the chart to read, and it refuses to render the component.
  embeddings_on = var.embeddings_enabled && var.embeddings_existing_secret != ""
}

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

  # --- Pod stats ---
  # Always set rather than set-when-true, so turning it back off is a change the
  # plan shows and applies, not a value that silently keeps whatever the last
  # apply left behind.
  set {
    name  = "orchestrator.podStats.enabled"
    value = var.pod_stats_enabled
  }

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
  # Bundled Postgres. The chart creates the StatefulSet and a Secret carrying the
  # username and database name; the password comes from a Secret the caller owns,
  # named here. Skipped entirely on an external-database install, which has no
  # chart-owned password at all.
  #
  # Both halves of that condition are load-bearing. A caller that sets both — by
  # switching an existing release to a managed instance without clearing the old
  # reference — would otherwise leave the chart reading a Secret belonging to a
  # StatefulSet that no longer exists.
  dynamic "set" {
    for_each = var.external_database == null && var.postgres_existing_secret != "" ? {
      "postgres.auth.existingSecret"            = var.postgres_existing_secret
      "postgres.auth.existingSecretPasswordKey" = var.postgres_existing_secret_password_key
    } : {}
    content {
      name  = set.key
      value = set.value
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

  # Whether kubelet may create that path. Only meaningful alongside it.
  dynamic "set" {
    for_each = var.postgres_host_path != "" && var.postgres_host_path_type != "" ? toset(["postgres.storage.hostPathType"]) : toset([])
    content {
      name  = set.value
      value = var.postgres_host_path_type
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

  # The controller that claims the editor's Ingress, and the one the orchestrator
  # names on every per-integration Ingress. Both from one variable: a cluster
  # running two controllers is a real arrangement (see the GCE-editor/nginx-apps
  # split in the GKE docs), but it is the exception, and extra_values expresses it
  # without making every caller carry two knobs.
  #
  # Empty sets neither, which is what a values_files profile needs in order to
  # decide — a `set` block would win over it. The chart's own default is likewise
  # empty (the cluster's default IngressClass claims it), so nothing here is
  # load-bearing except on a cluster that has no default and no profile.
  dynamic "set" {
    for_each = var.ingress_class != "" ? {
      "ingress.className"         = var.ingress_class
      "orchestrator.ingressClass" = var.ingress_class
    } : {}
    content {
      name  = set.key
      value = set.value
    }
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
  # When enabled the editor mounts OIDC_* / AUTH_SECRET. The issuer and client id
  # are not credentials and travel as plain values; the client secret and the
  # session secret are read from a Secret the caller created, named below.
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
      var.oidc_provider_name != "" ? { "auth.oidc.providerName" = var.oidc_provider_name } : {},
      var.oidc_write_roles != "" ? { "auth.writeRoles" = var.oidc_write_roles } : {},
      var.oidc_roles_claim != "" ? { "auth.rolesClaim" = var.oidc_roles_claim } : {},
    ) : {}
    content {
      name  = set.key
      value = set.value
    }
  }

  # The two credentials, by reference. Emitted only when SSO is on AND a Secret was
  # named: the chart requires both values when auth.oidc.enabled is true, so a root
  # that enables SSO without creating the Secret gets that error by name rather
  # than an editor that renders a sign-in button and fails at the provider.
  dynamic "set" {
    for_each = var.oidc_enabled && var.auth_existing_secret != "" ? {
      "auth.existingSecret"                = var.auth_existing_secret
      "auth.existingSecretClientSecretKey" = var.auth_existing_secret_client_secret_key
      "auth.existingSecretAuthSecretKey"   = var.auth_existing_secret_auth_secret_key
    } : {}
    content {
      name  = set.key
      value = set.value
    }
  }

  # --- The embedding server ---
  # Enabled only when a key was supplied: the chart refuses to render the
  # component without one, and an installation with no embeddings is an ordinary
  # one rather than a broken one.
  set {
    name  = "embeddings.enabled"
    value = local.embeddings_on
  }

  dynamic "set" {
    for_each = local.embeddings_on ? {
      "embeddings.connectorType"     = var.embeddings_connector_type
      "embeddings.model"             = var.embeddings_model
      "embeddings.dimensions"        = tostring(var.embeddings_dimensions)
      "embeddings.existingSecret"    = var.embeddings_existing_secret
      "embeddings.existingSecretKey" = var.embeddings_existing_secret_key
    } : {}
    content {
      name  = set.key
      value = set.value
    }
  }

  # KV secret-namespace encryption key, by reference. Named only when a Secret
  # exists, so a key-less install leaves encryption disabled (secret-namespace
  # writes are rejected, plain KV still works) rather than pointing the chart at
  # nothing.
  dynamic "set" {
    for_each = var.kv_existing_secret != "" ? {
      "kv.existingSecret"    = var.kv_existing_secret
      "kv.existingSecretKey" = var.kv_existing_secret_key
    } : {}
    content {
      name  = set.key
      value = set.value
    }
  }

  # Dev-run hash secret, by reference. Keys the HMAC deriving every dev run's
  # identity and public hostname, so the chart's orchestrator.devRuns.enabled (true
  # by default) refuses to render without it — a root that names no Secret gets
  # that error rather than a silently unkeyed install, which is the chart's design
  # and not something to paper over here. Named only when set for the same reason
  # as the KV key: the empty sentinel means "emit nothing", leaving whatever a
  # values file said.
  dynamic "set" {
    for_each = var.dev_runs_existing_secret != "" ? {
      "orchestrator.devRuns.existingSecret"    = var.dev_runs_existing_secret
      "orchestrator.devRuns.existingSecretKey" = var.dev_runs_existing_secret_key
    } : {}
    content {
      name  = set.key
      value = set.value
    }
  }
}
