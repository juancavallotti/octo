# A Cloud SQL for PostgreSQL instance on a private IP inside the caller's VPC, plus
# the octo database and its owner.
#
# Private IP rather than a public IP with authorized networks, for two reasons that
# are specific to this stack: GKE node IPs are ephemeral, so an allowlist would have
# to be rewritten every time the pool scales; and the octo chart has no Cloud SQL
# Auth Proxy sidecar seam, so a routable private address is what lets the DSN in
# helm/templates/_helpers.tpl work unmodified.
#
# It is reachable from pods because the cluster is VPC-native: pod IPs come from a
# subnet secondary range, and subnet routes are exported over the service-networking
# peering by default. The caller owns that peering (modules/gke-cluster,
# enable_private_services_access) and must order this module after it.

resource "google_project_service" "sqladmin" {
  project            = var.project_id
  service            = "sqladmin.googleapis.com"
  disable_on_destroy = false
}

# Alphanumeric only: the password is interpolated into a postgres:// DSN, and
# special characters would need percent-encoding there. Same rationale as the
# release root's random_password.postgres.
resource "random_password" "octo" {
  length  = 24
  special = false
}

resource "google_sql_database_instance" "this" {
  name                = var.name
  project             = var.project_id
  region              = var.region
  database_version    = var.database_version
  deletion_protection = var.deletion_protection

  settings {
    tier = var.tier
    # ZONAL is a single instance. REGIONAL adds a synchronous standby and roughly
    # doubles the bill — right for production, wasted on a cluster that exists to
    # be destroyed this afternoon.
    availability_type = var.high_availability ? "REGIONAL" : "ZONAL"
    disk_size         = var.disk_size_gb
    disk_type         = var.disk_type
    disk_autoresize   = true

    ip_configuration {
      # No public IP at all. The instance is only reachable over the peering.
      ipv4_enabled    = false
      private_network = var.network_id
      # Reject unencrypted connections. The chart connects with sslmode=require,
      # which encrypts without verifying the server certificate — so no CA bundle
      # has to be distributed into every pod. verify-ca/verify-full would.
      ssl_mode = "ENCRYPTED_ONLY"
    }

    backup_configuration {
      enabled = var.backup_enabled
      # Point-in-time recovery needs WAL archiving, which costs storage.
      point_in_time_recovery_enabled = var.backup_enabled
    }

    # Terraform is not the source of truth for how big this got.
    dynamic "insights_config" {
      for_each = var.query_insights ? [1] : []
      content {
        query_insights_enabled = true
      }
    }

    deletion_protection_enabled = var.deletion_protection
  }

  lifecycle {
    ignore_changes = [settings[0].disk_size]
  }

  depends_on = [google_project_service.sqladmin]
}

resource "google_sql_database" "octo" {
  name     = var.database_name
  project  = var.project_id
  instance = google_sql_database_instance.this.name
}

# This user owns `database_name`, and that is load-bearing. On PostgreSQL 16 the
# CREATE privilege on schema `public` is revoked from PUBLIC, so a non-owner cannot
# create tables — and the chart's schema Job does exactly that. A "least privilege"
# application user added here would make the Job fail on first install.
resource "google_sql_user" "octo" {
  name     = var.database_user
  project  = var.project_id
  instance = google_sql_database_instance.this.name
  password = random_password.octo.result
}
