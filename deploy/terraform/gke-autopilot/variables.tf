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

# --- Editor authentication ---
#
# The Auth.js session secret and the KV encryption key are not inputs: both are
# minted per cluster by ../modules/octo-gke and held in this root's state, because
# neither has anything to stay consistent with across a destroy/recreate cycle.

variable "oidc_enabled" {
  type        = bool
  description = "Require OIDC single sign-on to reach the editor. Off by default, which suits a cluster that exists for an afternoon — but it means anyone who can resolve the hostname gets in, so turn it on for anything left standing on a public domain. Needs oidc_client_id and oidc_client_secret."
  default     = false
}

variable "oidc_issuer" {
  type        = string
  description = "OIDC issuer URL of your identity provider (OIDC_ISSUER) — the base its .well-known/openid-configuration hangs off. Any OIDC provider works."
  default     = ""
}

variable "oidc_client_id" {
  type        = string
  description = "OIDC client id. Not a secret — it travels in the authorization redirect."
  default     = ""
}

variable "oidc_provider_name" {
  type        = string
  description = "How the sign-in button names your identity provider (\"Sign in with …\"). Empty renders the app default, \"OIDC\". The remaining display knobs (logo, scopes, endpoint overrides) are chart values rather than Terraform variables."
  default     = ""
}

variable "oidc_client_secret" {
  type        = string
  description = "OIDC client secret."
  default     = ""
  sensitive   = true
}

variable "oidc_write_roles" {
  type        = string
  description = "Comma-separated roles allowed to perform writes (e.g. \"admin,operator\"). Empty lets any signed-in user write."
  default     = ""
}

variable "oidc_roles_claim" {
  type        = string
  description = "id-token claim carrying roles. Empty uses the Auth.js default, \"roles\"."
  default     = ""
}

variable "private_nodes" {
  type        = bool
  description = "Give nodes no public IPs, adding a Cloud NAT for egress and the control-plane firewall rule the admission webhooks need. Off by default: the NAT gateway is a standing hourly charge, and public nodes reach Docker Hub, ghcr.io and Let's Encrypt directly."
  default     = false
}

variable "master_authorized_networks" {
  type        = list(string)
  description = "CIDRs allowed to reach the control-plane endpoint. Empty (the default) omits the allowlist, leaving GKE's behaviour for a public cluster: reachable from anywhere, and every connection still authenticates with IAM. Set it to your own address for anything that outlives a test — and remember that whatever runs `terraform apply` must be in the list, because the helm and kubernetes providers talk to that endpoint."
  default     = []
}

variable "deletion_protection" {
  type        = bool
  description = "Refuse to destroy the cluster. Off by default: the google provider defaults it ON, which makes `terraform destroy` fail on a cluster whose purpose is to be destroyed."
  default     = false
}

# --- The embedding server -----------------------------------------------------
#
# Optional. With no key there is no embedding server, and searching agent memory
# ranks by matching text rather than by meaning — a supported way to run, not a
# degraded one.
#
# The server holds the provider key so that no integration pod has to; every pod
# is given only its URL, which grants nothing an embedding could not already do.
#
# The model is deploy-time configuration rather than a setting: vectors carry no
# record of which model produced them, so a store holding two models' cannot be
# ranked coherently. Changing embeddings_model on an installation that has
# embedded anything discards those vectors and re-embeds the store.

variable "embeddings_enabled" {
  type        = bool
  description = "Deploy the embedding server, so searching agent memory ranks by meaning. Requires embeddings_api_key."
  default     = false
}

variable "embeddings_connector_type" {
  type        = string
  description = "Which provider connector the embedding server builds: llm-openai, llm-gemini or llm-openrouter. Not llm-anthropic — Anthropic has no embeddings API, and the server refuses to start rather than failing on the first call."
  default     = "llm-openai"
}

variable "embeddings_model" {
  type        = string
  description = "Provider-specific embedding model id. Changing it on an installation that has embedded anything discards every stored vector and re-embeds the store."
  default     = "text-embedding-3-small"
}

variable "embeddings_dimensions" {
  type        = number
  description = "Stored vector width. Must match the vector(N) columns in sql/schema.sql, which are indexed and so fixed at schema time."
  default     = 1536
}

variable "embeddings_api_key" {
  type        = string
  description = "Provider API key for the embedding server. Held in exactly one pod on the installation, which is the reason the server exists."
  default     = ""
  sensitive   = true
}
