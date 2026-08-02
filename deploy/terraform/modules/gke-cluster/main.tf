# A GKE cluster and the network it lives in, in Autopilot or Standard flavour.
#
# The network is created here rather than reusing `default`: the k3s VM's instance
# and firewall rules live on `default`, and a test cluster that gets destroyed and
# recreated has no business sharing blast radius with it. A dedicated VPC also lets
# the Cloud SQL peering below be torn down by simply deleting the VPC.
#
# The cluster is VPC-native in both flavours (mandatory on Autopilot). That is not
# an incidental detail: pod IPs come from a subnet secondary range, subnet routes
# are exported over the service-networking peering, and that is precisely why a
# pod can reach a Cloud SQL private IP with no proxy sidecar.

locals {
  # Autopilot is regional by definition. Standard defaults to zonal — one control
  # plane in one zone, which is both cheaper and quicker to create than a regional
  # one, and enough for a cluster whose purpose is a shakedown.
  location = var.autopilot || var.regional ? var.region : var.zone

  pods_range     = "pods"
  services_range = "services"
}

resource "google_project_service" "apis" {
  for_each = toset([
    "compute.googleapis.com",
    "container.googleapis.com",
    "servicenetworking.googleapis.com",
  ])
  service = each.value
  # Never disable on destroy: these roots are torn down and rebuilt routinely, and
  # other roots in the same project (the k3s VM) depend on compute.
  disable_on_destroy = false
}

# --- Network ---

resource "google_compute_network" "this" {
  name                    = var.name
  auto_create_subnetworks = false
  depends_on              = [google_project_service.apis]
}

resource "google_compute_subnetwork" "this" {
  name          = var.name
  network       = google_compute_network.this.id
  region        = var.region
  ip_cidr_range = var.subnet_cidr

  secondary_ip_range {
    range_name    = local.pods_range
    ip_cidr_range = var.pods_cidr
  }
  secondary_ip_range {
    range_name    = local.services_range
    ip_cidr_range = var.services_cidr
  }

  # So nodes reach Google APIs (Artifact Registry, the metadata server for
  # Workload Identity) without needing a public IP or a NAT hop.
  private_ip_google_access = true
}

# Egress for private nodes. Public nodes reach Docker Hub, ghcr.io and Let's
# Encrypt directly and need none of this — which is why private_nodes is off by
# default: a Cloud NAT gateway is a standing hourly charge for a test cluster.
resource "google_compute_router" "this" {
  count   = var.private_nodes ? 1 : 0
  name    = "${var.name}-router"
  region  = var.region
  network = google_compute_network.this.id
}

resource "google_compute_router_nat" "this" {
  count                              = var.private_nodes ? 1 : 0
  name                               = "${var.name}-nat"
  router                             = google_compute_router.this[0].name
  region                             = var.region
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"
}

# On a private cluster the control plane cannot reach admission webhooks unless
# this rule exists. Symptom without it: cert-manager and ingress-nginx install
# fine, then every subsequent create against their webhooks hangs until it times
# out — with nothing in any log that points at the firewall.
resource "google_compute_firewall" "master_webhooks" {
  count         = var.private_nodes ? 1 : 0
  name          = "${var.name}-master-webhooks"
  network       = google_compute_network.this.name
  direction     = "INGRESS"
  source_ranges = [var.master_ipv4_cidr]

  allow {
    protocol = "tcp"
    ports    = ["8443", "9443", "10250"]
  }
}

# The ingress controller's address, reserved up front. Reading it back off the
# LoadBalancer Service after the fact would mean the DNS records could not be
# created until the addons had settled; reserving it lets DNS, the cluster and the
# controller all come up in the same apply, and it survives the controller being
# reinstalled.
resource "google_compute_address" "ingress" {
  name         = "${var.name}-ingress"
  region       = var.region
  address_type = "EXTERNAL"
  depends_on   = [google_project_service.apis]
}

# --- Private Services Access, for Cloud SQL ---

resource "google_compute_global_address" "psa" {
  count         = var.enable_private_services_access ? 1 : 0
  name          = "${var.name}-psa"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.this.id
}

resource "google_service_networking_connection" "psa" {
  count                   = var.enable_private_services_access ? 1 : 0
  network                 = google_compute_network.this.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.psa[0].name]

  # Deleting a service-networking connection routinely fails with "Cannot modify
  # allocated ranges ... resources are in use" and wedges the entire destroy.
  # ABANDON leaves nothing stranded: the peering only exists on this VPC, and
  # deleting the VPC tears it down regardless.
  deletion_policy = "ABANDON"
}

# --- Cluster ---

resource "google_container_cluster" "this" {
  name     = var.name
  location = local.location
  project  = var.project_id

  network    = google_compute_network.this.id
  subnetwork = google_compute_subnetwork.this.id

  enable_autopilot = var.autopilot ? true : null

  # Standard: drop the default pool and manage one explicitly below. Both fields
  # conflict with Autopilot, hence null.
  remove_default_node_pool = var.autopilot ? null : true
  initial_node_count       = var.autopilot ? null : 1

  ip_allocation_policy {
    cluster_secondary_range_name  = local.pods_range
    services_secondary_range_name = local.services_range
  }

  # Autopilot has Workload Identity on permanently and rejects the block.
  dynamic "workload_identity_config" {
    for_each = var.autopilot ? [] : [1]
    content {
      workload_pool = "${var.project_id}.svc.id.goog"
    }
  }

  dynamic "private_cluster_config" {
    for_each = var.private_nodes ? [1] : []
    content {
      enable_private_nodes    = true
      enable_private_endpoint = false
      master_ipv4_cidr_block  = var.master_ipv4_cidr
    }
  }

  # The Terraform helm/kubernetes providers talk to the control plane over the
  # public endpoint, so whatever runs `apply` has to be in here.
  master_authorized_networks_config {
    dynamic "cidr_blocks" {
      for_each = var.master_authorized_networks
      content {
        cidr_block   = cidr_blocks.value
        display_name = "authorized-${cidr_blocks.key}"
      }
    }
  }

  release_channel {
    channel = var.release_channel
  }

  # Defaults to true in the google provider, which makes `terraform destroy` fail
  # outright. Wrong default for a cluster whose whole purpose is to be destroyed.
  deletion_protection = var.deletion_protection

  # Cloud SQL must be reachable the moment the first pod starts, and the schema
  # Job runs during the Helm install.
  depends_on = [google_service_networking_connection.psa]
}

# --- Node pool (Standard only) ---

resource "google_container_node_pool" "primary" {
  count    = var.autopilot ? 0 : 1
  name     = "${var.name}-pool"
  cluster  = google_container_cluster.this.id
  location = local.location

  # Per-zone when the cluster is regional, so a regional cluster does not silently
  # multiply the node count by three.
  initial_node_count = var.node_count

  autoscaling {
    min_node_count = var.node_min_count
    max_node_count = var.node_max_count
  }

  management {
    auto_repair  = true
    auto_upgrade = true
  }

  node_config {
    machine_type = var.machine_type
    # Spot nodes are ~60-90% cheaper and can be preempted at any time. For a test
    # cluster that is the right trade; set spot_nodes = false for anything real.
    spot         = var.spot_nodes
    disk_size_gb = var.node_disk_size_gb
    disk_type    = "pd-balanced"
    oauth_scopes = ["https://www.googleapis.com/auth/cloud-platform"]

    # Required for Workload Identity on Standard: without GKE_METADATA a pod gets
    # the node's service account from the legacy metadata server instead of its
    # own bound identity, and cert-manager's DNS-01 solver fails to authenticate.
    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    shielded_instance_config {
      enable_secure_boot          = true
      enable_integrity_monitoring = true
    }

    labels = {
      "octo-role" = "default"
    }
  }

  lifecycle {
    # The release channel upgrades nodes out from under Terraform; treating that
    # as drift would roll the pool on every apply.
    ignore_changes = [version]
  }
}
