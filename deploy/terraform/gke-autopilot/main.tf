# octo on GKE Autopilot: cluster, prerequisite controllers, DNS and the chart — one
# apply, from nothing to a working editor at https://{domain}.
#
#   task gke:autopilot:apply
#
# This exists to exercise helm/values-gke-autopilot.yaml on a real cluster. It is a
# TEST environment and its defaults say so: no Cloud NAT, no deletion protection, a
# Cloud SQL instance with no backups. Each has a variable that turns it into the
# production choice.
#
# There are no node settings here because Autopilot owns the nodes. That is also
# why helm_timeout defaults higher than on Standard: Autopilot may have to
# provision a node before the first pod can start at all, and the Helm timeout —
# not the chart's startupProbe — is what gives out first.
#
# The composition lives in ../modules/octo-gke, shared with the gke-standard root
# so the two cannot drift; this file is the flavour, the providers and the state.
#
# Tear down with `task gke:autopilot:destroy` — phased, because the ingress
# controller owns the load balancer and destroying it alongside the release strands
# a forwarding rule that then blocks the VPC delete.

data "google_client_config" "default" {}

module "octo_gke" {
  source = "../modules/octo-gke"

  autopilot = true

  project_id = var.project_id
  name       = var.name
  region     = var.region
  zone       = var.zone

  dns_managed_zone = var.dns_managed_zone
  domain           = var.domain
  apps_domain      = var.apps_domain
  acme_email       = var.acme_email
  acme_server      = var.acme_server

  namespace         = var.namespace
  chart_source      = var.chart_source
  chart_version     = var.chart_version
  image_values_file = var.image_values_file
  helm_timeout      = var.helm_timeout

  external_database = var.external_database
  cloudsql_tier     = var.cloudsql_tier

  oidc_enabled       = var.oidc_enabled
  oidc_issuer        = var.oidc_issuer
  oidc_client_id     = var.oidc_client_id
  oidc_client_secret = var.oidc_client_secret
  oidc_write_roles   = var.oidc_write_roles
  oidc_roles_claim   = var.oidc_roles_claim

  private_nodes              = var.private_nodes
  master_authorized_networks = var.master_authorized_networks
  deletion_protection        = var.deletion_protection
}
