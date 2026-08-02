# octo on GKE Standard: cluster, prerequisite controllers, DNS and the chart — one
# apply, from nothing to a working editor at https://{domain}.
#
#   task gke:standard:apply
#
# This exists to exercise helm/values-gke-standard.yaml on a real cluster. It is a
# TEST environment and its defaults say so: spot nodes, a zonal control plane, no
# Cloud NAT, no deletion protection, a Cloud SQL instance with no backups. Each has
# a variable that turns it into the production choice.
#
# The composition lives in ../modules/octo-gke, shared with the gke-autopilot root
# so the two cannot drift; this file is the flavour, the providers and the state.
#
# Tear down with `task gke:standard:destroy` — phased, because the ingress
# controller owns the load balancer and destroying it alongside the release strands
# a forwarding rule that then blocks the VPC delete.

data "google_client_config" "default" {}

module "octo_gke" {
  source = "../modules/octo-gke"

  autopilot = false

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

  machine_type      = var.machine_type
  node_count        = var.node_count
  node_min_count    = var.node_min_count
  node_max_count    = var.node_max_count
  node_disk_size_gb = var.node_disk_size_gb
  spot_nodes        = var.spot_nodes
  regional          = var.regional

  private_nodes              = var.private_nodes
  master_authorized_networks = var.master_authorized_networks
  deletion_protection        = var.deletion_protection
}
