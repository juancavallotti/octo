variable "project_id" {
  type        = string
  description = "GCP project (for the OCI chart token and SSH image pulls)."
}

variable "region" {
  type        = string
  description = "Artifact Registry region."
  default     = "us-west1"
}

variable "zone" {
  type        = string
  description = "Zone of the k3s VM (for the SSH image pull)."
  default     = "us-west1-a"
}

variable "instance_name" {
  type        = string
  description = "Name of the k3s VM (for the SSH image pull)."
  default     = "octo"
}

variable "repository_id" {
  type        = string
  description = "Artifact Registry repository holding the images and OCI chart."
  default     = "octo"
}

variable "namespace" {
  type        = string
  description = "Kubernetes namespace for the production release. Overrides the helm-release module default (octo-dev, the devspace local-dev namespace). Changing it forces the release to be recreated."
  default     = "octo"
}

variable "domain" {
  type        = string
  description = "Editor hostname; must match the k3s deployment. Per-integration subdomains live under *.{apps_domain}, which defaults to this domain — see apps_domain."
  default     = "octo.juancavallotti.com"
}

# Mirrors the infra root's apps_domain — see its comment there. Must resolve to the
# same effective value on both roots: the infra root creates the DNS records for it
# and this root's chart values point the wildcard cert at it.
variable "apps_domain" {
  type        = string
  description = "Base hostname per-integration subdomains live under (orchestrator.baseDomain) and the domain the wildcard cert covers. Empty = same as `domain`."
  default     = ""
}

variable "image_tag" {
  type        = string
  description = "Tag of the octo-* images to deploy (e.g. v0.1.1 or latest). Changing it re-pulls on the node and rolls the Deployments."
  default     = "latest"
}

variable "chart_version" {
  type        = string
  description = "Version of the octo chart in Artifact Registry OCI. Must match helm/Chart.yaml at the published release; derived from it by Cloud Build and `task deploy`, so it is required (no default) to avoid drift."
}

variable "ingress_class" {
  type        = string
  description = "IngressClass for the editor and every per-integration Ingress. traefik is the controller k3s bundles, and this root is the k3s path — so it is named here rather than defaulted in the chart, which is published for clusters running anything. Empty falls back to whichever IngressClass the cluster marks default."
  default     = "traefik"
}

variable "cluster_issuer" {
  type        = string
  description = "Per-host (HTTP-01) cert-manager ClusterIssuer created by the k3s bootstrap; used when wildcard_tls is false."
  default     = "letsencrypt-prod"
}

variable "wildcard_tls" {
  type        = bool
  description = "Issue one *.{domain} wildcard cert via DNS-01 and share it across the editor + per-integration ingresses, so subdomains validate. Requires the bootstrap's DNS-01 ClusterIssuer."
  default     = true
}

variable "kubeconfig" {
  type        = string
  description = "Path to the k3s kubeconfig (server rewritten to https://{domain}:6443). Produced by `task deploy:kubeconfig`."
  default     = ""
}

# --- OIDC SSO ---

variable "oidc_enabled" {
  type        = bool
  description = "Enable OIDC single sign-on for the editor."
  default     = false
}

variable "oidc_issuer" {
  type        = string
  description = "OIDC issuer URL of your identity provider (OIDC_ISSUER) — the base its .well-known/openid-configuration hangs off. Any OIDC provider works."
  default     = ""
}

variable "oidc_write_roles" {
  type        = string
  description = "Comma-separated roles allowed to perform writes; empty = any signed-in user."
  default     = ""
}

variable "oidc_roles_claim" {
  type        = string
  description = "id-token claim carrying roles (Auth.js default \"roles\")."
  default     = ""
}

variable "oidc_client_id" {
  type        = string
  description = "OIDC client id from the IdP (non-secret); passed to the chart."
  default     = ""
}

variable "oidc_provider_name" {
  type        = string
  description = "How the sign-in button names your identity provider (\"Sign in with …\"). Empty renders the app default, \"OIDC\". The remaining display knobs (logo, scopes, endpoint overrides) are chart values rather than Terraform variables."
  default     = ""
}

variable "oidc_client_secret" {
  type        = string
  description = "OIDC client secret from the IdP; passed to the chart (lands in release state, not Secret Manager)."
  default     = ""
  sensitive   = true
}

# --- The embedding server -----------------------------------------------------
#
# Supplied here on a local `task deploy`, and persisted to the state bucket so a
# Cloud Build deploy — which has no terraform.tfvars — can read the key back. The
# same mechanism the OIDC credentials use, and for the same reason.

variable "embeddings_enabled" {
  type        = bool
  description = "Deploy the embedding server so agent-memory search ranks by meaning. Requires embeddings_api_key. Off leaves search matching text."
  default     = false
}

variable "embeddings_connector_type" {
  type        = string
  description = "llm-openai, llm-gemini or llm-openrouter. Anthropic has no embeddings API and is refused at flow-build time."
  default     = "llm-openai"
}

variable "embeddings_model" {
  type        = string
  description = "Provider-specific embedding model id. Changing it on an installation that has embedded anything discards those vectors and re-embeds the store — vectors carry no record of which model made them, so one embedding space at a time is what keeps the store searchable."
  default     = "text-embedding-3-small"
}

variable "embeddings_dimensions" {
  type        = number
  description = "Stored vector width; must match the vector(N) columns in sql/schema.sql."
  default     = 1536
}

variable "embeddings_api_key" {
  type        = string
  description = "Provider API key for the embedding server (lands in release state and in the state bucket, not Secret Manager — the same treatment oidc_client_secret gets). Empty on a Cloud Build deploy, which reads the stored one back."
  default     = ""
  sensitive   = true
}

variable "image_values_file" {
  type        = string
  description = "Path to the digest-pinned image values file rendered by cloudbuild.yaml (render-image-values). Cloud Build passes /workspace/dist/values.images.yaml so the release installs exactly the images that build pushed; a local `task deploy` leaves it empty and identifies images by image_tag instead."
  default     = ""
}

variable "pod_stats_enabled" {
  type        = bool
  description = "Collect per-pod CPU, memory and runtime metrics from every deployed integration. On by default for this root: it is what the platform's metrics views read, and an install that ships them with nothing behind them looks broken rather than unconfigured."
  default     = true
}

variable "values_files" {
  type        = list(string)
  description = "Extra chart values files to layer in, lowest precedence first. Empty by default: the single-node k3s VM this root targets is what the chart's own defaults describe, so no profile is needed."
  default     = []
}

variable "postgres_host_path" {
  type        = string
  description = <<-EOT
    Node path to pin the bundled Postgres to on the k3s VM. Setting "" instead leaves
    the volume dynamically provisioned by k3s's local-path provisioner, whose per-claim
    directory name changes whenever the claim does — so a re-bootstrap of the VM
    (`rm /opt/octo/.provisioned && reboot`, which reinstalls k3s) brings the database
    back empty with the old data stranded on disk.

    The default is the data disk the infra root attaches and its startup script mounts
    (modules/base, /mnt/octo-data). That disk is a resource of its own and is not
    deleted with the instance, so the database outlives a VM rebuild as well as a k3s
    reinstall — which a path on the boot disk would not. It must sit under the infra
    root's data_disk_mount_path; that root outputs the exact value to use as
    `postgres_host_path`.

    CHANGING this on a release that already holds data is a data move, not a config
    change: the next apply points Postgres at the new path and the database comes up
    empty. Move the data first, with the workload stopped — see docs/deployment.md.

    UPGRADING an install that predates the data disk: this default is new, and it
    applies itself. A Cloud Build deploy runs without release/terraform.tfvars, so
    the next automated deploy of an existing cluster silently becomes exactly the
    data move described above. Before taking this version:

      1. `task infra:apply` — creates and mounts the disk (nothing else moves).
      2. Copy the data across per docs/deployment.md, workload stopped.

    Not ready to do that yet? Pin `postgres_host_path = ""` in release/terraform.tfvars
    AND in the Cloud Build substitutions, and keep the old behaviour until you are.
  EOT
  default     = "/mnt/octo-data/postgres"
}
