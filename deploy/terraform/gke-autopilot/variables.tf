# Variables for the `gke-autopilot` root. Values come from terraform.tfvars,
# which Terraform loads automatically — see terraform.tfvars.example.


variable "project_id" {
  type        = string
  description = "GCP project to create the cluster, its network and its DNS records in."
}

variable "name" {
  type        = string
  description = "Prefix for every cloud resource: the cluster, VPC, subnet, ingress IP, service accounts and the Cloud SQL instance."
  default     = "octo-gke-ap"
}

variable "region" {
  type        = string
  description = "Region for the network, the ingress IP and Cloud SQL."
  default     = "us-west1"
}

variable "zone" {
  type        = string
  description = "Zone for the control plane and nodes when regional = false."
  default     = "us-west1-a"
}

variable "dns_managed_zone" {
  type        = string
  description = "Name (not DNS name) of the Cloud DNS zone the records go in. Created by hand once and delegated from the registrar — this root creates records in it but never the zone itself, so a destroy cannot take the delegation with it."
  default     = "gke-octopaas-dev"
}

variable "domain" {
  type        = string
  description = "Editor hostname. Gets its own per-host certificate via HTTP-01."
  default     = "octo.gke.octopaas.dev"
}

variable "apps_domain" {
  type        = string
  description = "Base hostname per-integration endpoints live under ({slug}.{apps_domain}). Gets an A record, a wildcard A record, and a *.{apps_domain} certificate via DNS-01. Empty disables all three and the wildcard cert with them."
  default     = "gke.octopaas.dev"
}

variable "acme_email" {
  type        = string
  description = "Email on the Let's Encrypt ACME account (expiry notices)."
}

variable "acme_server" {
  type        = string
  description = "ACME directory URL. Switch to https://acme-staging-v02.api.letsencrypt.org/directory while iterating: production allows only 5 duplicate certificates per week, which repeated create/destroy cycles burn through fast."
  default     = "https://acme-v02.api.letsencrypt.org/directory"
}

variable "namespace" {
  type        = string
  description = "Namespace for the release."
  default     = "octo"
}

variable "chart_source" {
  type        = string
  description = "local installs ../../../helm straight from this checkout, so a chart edit is one apply away with no package/publish/version-bump round trip. oci installs the published chart from ghcr.io, which is what actually verifies a release artifact."
  default     = "local"

  validation {
    condition     = contains(["local", "oci"], var.chart_source)
    error_message = "chart_source must be \"local\" or \"oci\"."
  }
}

variable "chart_version" {
  type        = string
  description = "Chart version to install when chart_source = oci. Ignored for a local path, which is whatever its Chart.yaml says."
  default     = null
}

variable "image_values_file" {
  type        = string
  description = "Values file naming the images to run, as rendered by `task images:ttl` (dist/values.ttl.yaml) or `task helm:values:images`. Empty falls through to the chart's own public Docker Hub defaults at its appVersion — which is only correct when the chart and the published images are from the same release."
  default     = ""
}

variable "helm_timeout" {
  type        = number
  description = "Helm install/upgrade timeout in seconds."
  default     = 1800
}

variable "external_database" {
  type        = bool
  description = "Use a Terraform-provisioned Cloud SQL instance on a private IP instead of the chart's bundled Postgres StatefulSet. The one flip between the two database paths; it also turns on the VPC's service-networking peering."
  default     = false
}

variable "cloudsql_tier" {
  type        = string
  description = "Cloud SQL machine tier when external_database is on."
  default     = "db-g1-small"
}

variable "private_nodes" {
  type        = bool
  description = "Give nodes no public IPs, adding a Cloud NAT for egress and the control-plane firewall rule the admission webhooks need. Off by default: the NAT gateway is a standing hourly charge, and public nodes reach Docker Hub, ghcr.io and Let's Encrypt directly."
  default     = false
}

variable "master_authorized_networks" {
  type        = list(string)
  description = "CIDRs allowed to reach the control plane. Whatever runs `terraform apply` must be in here — the helm and kubernetes providers talk to the public endpoint. Narrow it to your IP for anything long-lived."
  default     = ["0.0.0.0/0"]
}

variable "deletion_protection" {
  type        = bool
  description = "Refuse to destroy the cluster. Off by default: the google provider defaults it ON, which makes `terraform destroy` fail on a cluster whose purpose is to be destroyed."
  default     = false
}
