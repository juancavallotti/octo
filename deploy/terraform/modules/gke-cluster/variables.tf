variable "project_id" {
  type        = string
  description = "GCP project to create the cluster in."
}

variable "name" {
  type        = string
  description = "Name for the cluster and every network resource around it."
}

variable "region" {
  type        = string
  description = "Region for the subnet, the ingress IP and (for Autopilot or a regional Standard cluster) the control plane."
  default     = "us-west1"
}

variable "zone" {
  type        = string
  description = "Zone for a zonal Standard cluster. Ignored for Autopilot, which is always regional."
  default     = "us-west1-a"
}

variable "autopilot" {
  type        = bool
  description = "Run GKE Autopilot: Google manages the nodes, pods are billed on their resource requests, and hostPath/hostPort are rejected outright."
  default     = false
}

variable "regional" {
  type        = bool
  description = "Standard only: spread the control plane (and one node pool per zone) across the region. Off by default — a zonal cluster is cheaper, faster to create, and enough for a shakedown."
  default     = false
}

variable "release_channel" {
  type        = string
  description = "GKE release channel. RAPID gets the newest Kubernetes, STABLE the most conservative."
  default     = "REGULAR"
}

variable "deletion_protection" {
  type        = bool
  description = "Refuse to destroy the cluster. Off by default: the google provider defaults it ON, which makes `terraform destroy` fail on a cluster whose entire purpose is to be destroyed."
  default     = false
}

# --- Network ---

variable "subnet_cidr" {
  type        = string
  description = "Primary subnet range (node IPs)."
  default     = "10.60.0.0/20"
}

variable "pods_cidr" {
  type        = string
  description = "Secondary range for pod IPs. Being a subnet secondary range is what makes pods routable over the Cloud SQL peering."
  default     = "10.61.0.0/16"
}

variable "services_cidr" {
  type        = string
  description = "Secondary range for ClusterIP services."
  default     = "10.62.0.0/20"
}

variable "private_nodes" {
  type        = bool
  description = "Give nodes no public IPs, adding a Cloud NAT for egress and the control-plane firewall rule the admission webhooks need. Off by default: the NAT gateway is a standing hourly charge a test cluster does not need."
  default     = false
}

variable "master_ipv4_cidr" {
  type        = string
  description = "/28 for the control plane when private_nodes is on. Must not overlap any other range here."
  default     = "172.16.60.0/28"
}

variable "master_authorized_networks" {
  type        = list(string)
  description = "CIDRs allowed to reach the control plane. Whatever runs `terraform apply` must be in here — the helm and kubernetes providers talk to the public endpoint. Defaults open, matching the k3s root's ssh_source_ranges default; narrow it to your IP for anything long-lived."
  default     = ["0.0.0.0/0"]
}

variable "enable_private_services_access" {
  type        = bool
  description = "Reserve a peering range and connect it to servicenetworking, so a Cloud SQL instance can be given a private IP inside this VPC. Only needed on the external-database path."
  default     = false
}

# --- Node pool (Standard only) ---

variable "machine_type" {
  type        = string
  description = "Node machine type. The chart's GKE Standard profile requests roughly 2 vCPU / 6Gi across its workloads before ingress-nginx and cert-manager, so two e2-standard-2 is the comfortable floor."
  default     = "e2-standard-2"
}

variable "node_count" {
  type        = number
  description = "Initial nodes per zone."
  default     = 2
}

variable "node_min_count" {
  type        = number
  description = "Autoscaling floor, per zone."
  default     = 1
}

variable "node_max_count" {
  type        = number
  description = "Autoscaling ceiling, per zone."
  default     = 3
}

variable "node_disk_size_gb" {
  type        = number
  description = "Node boot disk size."
  default     = 50
}

variable "spot_nodes" {
  type        = bool
  description = "Use Spot VMs: far cheaper, preemptible at any moment. The right trade for a test cluster; turn it off for anything real."
  default     = true
}
