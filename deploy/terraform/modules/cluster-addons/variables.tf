variable "project_id" {
  type        = string
  description = "GCP project. Used for the cert-manager service account, its DNS admin binding, and the Cloud DNS project the DNS-01 solver writes into."
}

variable "name" {
  type        = string
  description = "Prefix for cloud resources created here (the cert-manager service account)."
}

variable "load_balancer_ip" {
  type        = string
  description = "Reserved external IP for the ingress controller's Service (modules/gke-cluster outputs it as ingress_ip). Reserving it up front is what lets the DNS records be created in the same apply."
}

variable "acme_email" {
  type        = string
  description = "Email on the Let's Encrypt ACME account (expiry notices)."
}

variable "acme_server" {
  type        = string
  description = "ACME directory URL. Point at https://acme-staging-v02.api.letsencrypt.org/directory while iterating — staging certs are untrusted but its rate limits are far more forgiving, and production allows only 5 duplicate certificates per week, which a repeatedly recreated test cluster burns through quickly."
  default     = "https://acme-v02.api.letsencrypt.org/directory"
}

variable "cluster_issuer_name" {
  type        = string
  description = "Name of the HTTP-01 ClusterIssuer. Matches the k3s VM's issuer name so the chart values are identical across deployment targets."
  default     = "letsencrypt-prod"
}

variable "enable_dns01" {
  type        = bool
  description = "Create the DNS-01 ClusterIssuer and the Workload Identity binding it needs. Required for the *.{apps_domain} wildcard certificate — DNS-01 is the only ACME challenge that can issue one."
  default     = true
}

variable "dns01_cluster_issuer_name" {
  type        = string
  description = "Name of the DNS-01 ClusterIssuer (the chart's wildcardTLS.clusterIssuer)."
  default     = "letsencrypt-dns"
}

variable "ingress_nginx_version" {
  type        = string
  description = "ingress-nginx chart version. Pinned so a cluster rebuild installs the controller that was tested, not whatever is newest."
  default     = "4.12.1"
}

variable "cert_manager_version" {
  type        = string
  description = "cert-manager chart version. Pinned for the same reason as ingress-nginx."
  default     = "v1.17.1"
}

variable "ingress_nginx_extra_set" {
  type        = map(string)
  description = "Extra Helm --set values for ingress-nginx, e.g. cloud-specific Service annotations."
  default     = {}
}

variable "timeout" {
  type        = number
  description = "Helm timeout in seconds for the controller installs. Generous by default: on Autopilot a node may have to be provisioned before the first pod can start at all."
  default     = 900
}
