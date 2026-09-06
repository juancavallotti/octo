variable "release_name" {
  type        = string
  description = "Helm release name."
  default     = "octo"
}

variable "namespace" {
  type        = string
  description = "Namespace for the release."
  default     = "octo-dev"
}

variable "create_namespace" {
  type        = bool
  description = "Let Helm create the namespace. Set false when the caller creates it itself — an external-database install must put the credential Secret in place before the chart installs, which means the namespace is a resource of its own."
  default     = true
}

# --- Chart source ---
#
# Three shapes are supported, and the difference is only these three variables:
#
#   Artifact Registry OCI  repository = "oci://REGION-docker.pkg.dev/PROJECT/octo"
#                          chart      = "octo", version = "0.6.4", + credentials
#   public ghcr.io OCI     repository = "oci://ghcr.io/juancavallotti/charts"
#                          chart      = "octo", version = "0.6.4", no credentials
#   local working tree     repository = "" , chart = "../../../helm", version = null
#
# The local path is what the cloud test roots use by default: a chart edit is one
# apply away, with no package/publish/version-bump round trip in between.

variable "repository" {
  type        = string
  description = "Chart repository URL (oci://... for an OCI registry). Empty means `chart` is a local filesystem path."
  default     = ""
}

variable "chart" {
  type        = string
  description = "Chart name within `repository`, or — when `repository` is empty — a local path to the chart directory."
  default     = "octo"
}

variable "chart_version" {
  type        = string
  description = "Chart version to install. Ignored (and must be null) for a local path, which has whatever version its Chart.yaml says."
  default     = null
}

variable "registry_username" {
  type        = string
  description = "Username for OCI chart pulls (oauth2accesstoken for Artifact Registry). Empty for a public registry — the helm provider only logs in when both a username and a password are set."
  default     = ""
}

variable "registry_password" {
  type        = string
  description = "Token/password for OCI chart pulls (a GCP access token for Artifact Registry). Empty for a public registry."
  default     = ""
  sensitive   = true
}

# --- Images ---

variable "image_registry" {
  type        = string
  description = "Registry the octo-* images are pulled from (chart value image.registry). Empty leaves the chart's own default (docker.io/juancavallotti), which is what a public install wants."
  default     = ""
}

variable "image_tag" {
  type        = string
  description = "Tag of the octo-* images to run (e.g. v0.1.1 or latest). Empty falls back to the chart's appVersion. Not passed at all when image_values_file is set."
  default     = ""
}

variable "image_pull_policy" {
  type        = string
  description = "chart value image.pullPolicy. Empty emits nothing, leaving whichever values file is in play to decide — which is what ttl.sh test images need, since they reuse a tag and must be re-pulled."
  default     = "IfNotPresent"
}

variable "image_values_file" {
  type        = string
  description = "Path to a values file pinning each octo image by digest, as rendered by cloudbuild.yaml's render-image-values step (or by `task images:ttl`). When set it supplies the image coordinates and image_tag is not passed to the chart at all, so the release runs exactly the images that build pushed. Empty falls back to tagging by image_tag."
  default     = ""
}

# --- Values layering ---

variable "values_files" {
  type        = list(string)
  description = "Extra chart values files to layer in, lowest precedence first (e.g. an environment profile shipped with the chart). Applied beneath the module's own settings, which are passed as Helm --set and therefore win."
  default     = []
}

variable "extra_values" {
  type        = list(string)
  description = <<-EOT
    Raw YAML documents layered on top of values_files (and beneath image_values_file).
    The escape hatch for values this module has no opinion about — in particular maps
    whose keys contain dots, which Helm's --set escaping renders unreadable:

      extra_values = [yamlencode({
        orchestrator = { ingressAnnotations = {
          "alb.ingress.kubernetes.io/certificate-arn" = aws_acm_certificate.octo.arn
        } }
      })]

    Anything this module sets explicitly below still wins, because Helm applies
    --set after every values file.
  EOT
  default     = []
}

# --- Networking / hostnames ---

variable "domain" {
  type        = string
  description = "Editor hostname (ingress.host). Always gets its own per-host TLS cert, independent of apps_domain."
}

variable "apps_domain" {
  type        = string
  description = "Base hostname per-integration subdomains live under (orchestrator.baseDomain) and the domain the wildcard cert covers. May differ from `domain`."
}

variable "ingress_enabled" {
  type        = bool
  description = "Create the editor Ingress."
  default     = true
}

variable "ingress_class" {
  type        = string
  description = "IngressClass for the editor Ingress and every per-integration Ingress the orchestrator creates (chart values ingress.className and orchestrator.ingressClass). Empty emits neither, so the cluster's default IngressClass claims them — or an environment profile in values_files decides, which is how the GKE (nginx) and EKS (alb) paths set it. Set it where there is no profile and no cluster default: the k3s VM, whose bundled controller is traefik."
  default     = ""
}

# --- TLS ---

variable "cluster_issuer" {
  type        = string
  description = "cert-manager ClusterIssuer for per-host (HTTP-01) TLS, used when wildcard_tls is false. Empty emits nothing — the EKS path terminates TLS at the ALB with an ACM certificate and has no cert-manager at all."
  default     = "letsencrypt-prod"
}

variable "wildcard_tls" {
  type        = bool
  description = "Issue one *.{apps_domain} wildcard cert via DNS-01 and have the editor + per-integration ingresses share it (so subdomains validate). Requires the DNS-01 ClusterIssuer and DNS admin on the zone."
  default     = true
}

variable "wildcard_cluster_issuer" {
  type        = string
  description = "DNS-01 cert-manager ClusterIssuer that issues the wildcard cert (created by the VM bootstrap, or by modules/cluster-addons on GKE)."
  default     = "letsencrypt-dns"
}

variable "tls_mode" {
  type        = string
  description = "chart value ingress.tls.mode — one of cert-manager, secret, gke-managed-cert, acm, none. Empty lets the chart auto-select (cert-manager when a clusterIssuer is set, otherwise secret), which is what every existing path relies on."
  default     = ""

  validation {
    condition     = contains(["", "cert-manager", "secret", "gke-managed-cert", "acm", "none"], var.tls_mode)
    error_message = "tls_mode must be one of: cert-manager, secret, gke-managed-cert, acm, none (or empty to auto-select)."
  }
}

variable "certificate_arn" {
  type        = string
  description = "ACM certificate ARN for tls_mode = acm; becomes the ALB's certificate-arn annotation on the editor Ingress."
  default     = ""
}

# --- Database ---

# --- Credential references -----------------------------------------------------
#
# Names and keys, never values. Helm keeps every value it is given in the release
# history, so a credential passed as a chart value is in the cluster for as long
# as the revision carrying it is retained. modules/octo-secrets creates these
# Secrets and outputs exactly these references; an empty name emits nothing and
# leaves whatever a values file said.

variable "postgres_existing_secret" {
  type        = string
  description = "Name of a Secret holding the bundled Postgres password (chart value postgres.auth.existingSecret). Empty emits nothing, which is what an external-database install wants — see external_database. The chart still creates its own Secret for the username and database name; only the password comes from this one."
  default     = ""
}

variable "postgres_existing_secret_password_key" {
  type        = string
  description = "Key within postgres_existing_secret holding the password."
  default     = "postgres-password"
}

variable "postgres_host_path" {
  type        = string
  description = <<-EOT
    Node path to pin the bundled Postgres to (chart value postgres.storage.hostPath).
    Empty provisions the volume dynamically, which on a single-node k3s cluster means
    the local-path provisioner: it names each volume's directory after the claim's UID,
    so reinstalling k3s or recreating the claim silently hands Postgres an empty
    directory while the old one stays on disk. A fixed path removes the UID from the
    equation and the data survives.

    Only as durable as what backs it: a path on the boot disk outlives a k3s reinstall
    but not the VM. Point it at a mount on an attached data disk (modules/base creates
    one at /mnt/octo-data) to survive VM replacement.

    Leave empty on any managed cluster — hostPath is node-local and GKE Autopilot
    rejects it outright.

    Changing this on a release that already holds data is a data move, not a config
    change: the next apply points Postgres at the new path and the database comes up
    empty. Copy the directory across first, with the workload stopped.
  EOT
  default     = ""
}

variable "postgres_host_path_type" {
  type        = string
  description = <<-EOT
    PersistentVolume hostPath type for postgres_host_path (chart value
    postgres.storage.hostPathType). Ignored when postgres_host_path is empty.

    Directory is the right answer whenever the path is a mount point on a separate
    disk, which is what modules/base creates. Under the chart's DirectoryOrCreate
    default, a data disk that failed to mount is invisible: kubelet creates the
    directory on the root filesystem, Postgres initialises an empty database there,
    and the install looks healthy right up until the disk is needed. Directory
    refuses the mount instead, so the pod stays Pending with the missing path named
    in its events.

    Empty leaves the chart's default, which is what a k3d or kind cluster wants —
    there the path is a bind mount that may legitimately not exist yet.
  EOT
  default     = ""

  validation {
    condition     = contains(["", "Directory", "DirectoryOrCreate"], var.postgres_host_path_type)
    error_message = "postgres_host_path_type must be Directory or DirectoryOrCreate (or empty for the chart default)."
  }
}

variable "external_database" {
  type = object({
    host                         = string
    port                         = optional(number, 5432)
    user                         = optional(string, "octo")
    database                     = optional(string, "octo")
    sslmode                      = optional(string, "require")
    existing_secret              = string
    existing_secret_password_key = optional(string, "password")
  })
  description = <<-EOT
    A managed Postgres (Cloud SQL, RDS) to use instead of the chart's bundled
    StatefulSet. Non-null sets postgres.enabled=false and the externalDatabase.*
    values; the schema Job then runs against this host instead.

    existing_secret is required rather than optional: the alternative is passing the
    password as a Helm value, and Helm keeps every value in release history. The
    caller creates the Secret (with the kubernetes provider) and names it here.
  EOT
  default     = null
}

# --- Release behaviour ---

variable "timeout" {
  type        = number
  description = "Helm install/upgrade timeout in seconds. Managed clusters want more than the k3s default: GKE Autopilot may provision a node before the first pod can even start."
  default     = 600
}

variable "atomic" {
  type        = bool
  description = "Roll back on a failed install/upgrade instead of leaving a half-applied release behind. Off by default so the k3s path keeps the failed state available for inspection; the cloud test roots turn it on."
  default     = false
}

# --- OIDC SSO ---

variable "oidc_enabled" {
  type        = bool
  description = "Enable OIDC SSO in the chart (creates the auth Secret + editor env)."
  default     = false
}

variable "oidc_issuer" {
  type        = string
  description = "OIDC issuer URL of your identity provider (OIDC_ISSUER) — the base its .well-known/openid-configuration hangs off. Any OIDC provider works."
  default     = ""
}

variable "oidc_client_id" {
  type        = string
  description = "OIDC client id (non-secret)."
  default     = ""
}

variable "oidc_provider_name" {
  type        = string
  description = "How the sign-in button names your identity provider (\"Sign in with …\"). Empty renders the app default, \"OIDC\". The remaining display knobs (logo, scopes, endpoint overrides) are chart values rather than Terraform variables."
  default     = ""
}

variable "auth_existing_secret" {
  type        = string
  description = "Name of a Secret holding BOTH the OIDC client secret and the Auth.js session secret (chart value auth.existingSecret). Required in practice when oidc_enabled is true: the chart refuses to render an SSO install with neither this nor the inline values, and the inline values are not something this module will pass."
  default     = ""
}

variable "auth_existing_secret_client_secret_key" {
  type        = string
  description = "Key within auth_existing_secret holding the OIDC client secret."
  default     = "oidc-client-secret"
}

variable "auth_existing_secret_auth_secret_key" {
  type        = string
  description = "Key within auth_existing_secret holding the Auth.js session secret."
  default     = "auth-secret"
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

variable "kv_existing_secret" {
  type        = string
  description = "Name of a Secret holding the base64-encoded 32-byte AES-256 key that encrypts KV secret namespaces at rest (chart value kv.existingSecret). Empty disables encryption: secret-namespace writes are rejected and plain KV still works."
  default     = ""
}

variable "kv_existing_secret_key" {
  type        = string
  description = "Key within kv_existing_secret holding the encryption key."
  default     = "kv-encryption-key"
}

# --- The embedding server -----------------------------------------------------
#
# Optional, and off is a supported way to run: with no key there is no embedding
# server, and searching agent memory ranks by matching text rather than by
# meaning. Nothing else changes.
#
# The model is deploy-time configuration rather than an admin setting, and
# deliberately so. Vectors carry no record of which model produced them, so a
# store holding two models' vectors cannot be ranked coherently — one embedding
# space at a time is what makes that safe. Changing embeddings_model on an
# installation that has embedded anything discards those vectors and re-embeds
# the store, at whatever the provider charges for it.

variable "pod_stats_enabled" {
  type        = bool
  description = "Inject the pod stats sidecar into every deployed integration, so each pod records its own CPU, memory and runtime metrics to Redis and the platform's metrics views have something to draw. Needs a Redis, which this chart bundles. Turning it on rolls every deployment in the namespace once, because the runtime container gains OCTO_METRICS=true."
  default     = false
}

variable "embeddings_enabled" {
  type        = bool
  description = "Deploy the embedding server, which turns text into vectors for agent-memory search. Requires embeddings_api_key. Off leaves search matching text, which is supported rather than degraded."
  default     = false
}

variable "embeddings_connector_type" {
  type        = string
  description = "Which provider connector the embedding server builds: llm-openai, llm-gemini or llm-openrouter. Anthropic is not an option — it has no embeddings API, and the flow is refused at build time rather than failing on the first call."
  default     = "llm-openai"
}

variable "embeddings_model" {
  type        = string
  description = "Provider-specific embedding model id. Read the note above before changing it on an installation that has already embedded anything."
  default     = "text-embedding-3-small"
}

variable "embeddings_dimensions" {
  type        = number
  description = "Stored vector width. Must match the vector(N) columns in sql/schema.sql, which are indexed and therefore fixed at schema time. It is requested of the provider; a model that cannot produce this width fails the call and says so."
  default     = 1536
}

variable "embeddings_existing_secret" {
  type        = string
  description = "Name of a Secret holding the embedding server's provider API key (chart value embeddings.existingSecret). Empty deploys no embedding server even when embeddings_enabled is true — there would be no key for it to read. The key is held in exactly one pod on the installation, which is the reason the server exists: the alternative is a key in every integration's pod."
  default     = ""
}

variable "embeddings_existing_secret_key" {
  type        = string
  description = "Key within embeddings_existing_secret holding the provider API key."
  default     = "apiKey"
}

variable "dev_runs_existing_secret" {
  type        = string
  description = "Name of a Secret holding the HMAC key that derives every dev run's identity and public hostname (chart value orchestrator.devRuns.existingSecret). Required by the chart whenever orchestrator.devRuns.enabled is true, which is the default. Mint the key once and never rotate it — rotating re-labels every exposed dev run and silently breaks webhooks registered against the old hostname."
  default     = ""
}

variable "dev_runs_existing_secret_key" {
  type        = string
  description = "Key within dev_runs_existing_secret holding the HMAC key."
  default     = "dev-run-hash-secret"
}
