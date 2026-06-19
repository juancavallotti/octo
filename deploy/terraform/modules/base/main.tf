# Shared single-VM infrastructure: APIs, service account, .env secret, static
# IP, firewall, the instance itself, and the DNS A record(s). The workload (what
# actually runs on the VM) is injected via var.startup_script + var.metadata.

resource "google_project_service" "apis" {
  for_each = toset([
    "compute.googleapis.com",
    "dns.googleapis.com",
    "secretmanager.googleapis.com",
  ])
  service            = each.value
  disable_on_destroy = false
}

resource "google_service_account" "vm" {
  account_id   = "${var.instance_name}-vm"
  display_name = "octo VM (${var.instance_name})"
}

# --- Secret: the whole .env stored as one unit ---
resource "google_secret_manager_secret" "env" {
  secret_id = var.secret_id

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "env" {
  secret      = google_secret_manager_secret.env.id
  secret_data = var.secret_data
}

resource "google_secret_manager_secret_iam_member" "vm_access" {
  secret_id = google_secret_manager_secret.env.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.vm.email}"
}

# --- Networking ---
resource "google_compute_address" "static" {
  name       = "${var.instance_name}-ip"
  region     = var.region
  depends_on = [google_project_service.apis]
}

resource "google_compute_firewall" "web" {
  name    = "${var.instance_name}-allow-web"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = var.web_tcp_ports
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = [var.instance_name]
}

resource "google_compute_firewall" "ssh" {
  name    = "${var.instance_name}-allow-ssh"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = var.ssh_source_ranges
  target_tags   = [var.instance_name]
}

# k3s API (6443), restricted: only created when source ranges are given, so the
# Terraform Helm provider can reach the cluster without exposing it to the world.
resource "google_compute_firewall" "kube_api" {
  count   = length(var.kube_api_source_ranges) > 0 ? 1 : 0
  name    = "${var.instance_name}-allow-kube-api"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["6443"]
  }

  source_ranges = var.kube_api_source_ranges
  target_tags   = [var.instance_name]
}

# --- The VM ---
resource "google_compute_instance" "vm" {
  name         = var.instance_name
  machine_type = var.machine_type
  zone         = var.zone
  tags         = [var.instance_name]

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
      size  = var.boot_disk_size_gb
    }
  }

  network_interface {
    network = "default"
    access_config {
      nat_ip = google_compute_address.static.address
    }
  }

  service_account {
    email  = google_service_account.vm.email
    scopes = ["cloud-platform"]
  }

  metadata                = var.metadata
  metadata_startup_script = var.startup_script

  depends_on = [
    google_secret_manager_secret_version.env,
    google_secret_manager_secret_iam_member.vm_access,
    google_project_service.apis,
  ]
}

# --- DNS A records ---
data "google_dns_managed_zone" "zone" {
  name = var.dns_managed_zone
}

resource "google_dns_record_set" "a" {
  name         = "${var.domain}."
  type         = "A"
  ttl          = 300
  managed_zone = data.google_dns_managed_zone.zone.name
  rrdatas      = [google_compute_address.static.address]
}

# Wildcard so per-integration subdomains ({slug}.{domain}) resolve to the same
# node; cert-manager then issues a per-host cert via HTTP-01 (Stage 2).
resource "google_dns_record_set" "wildcard" {
  count        = var.wildcard_dns ? 1 : 0
  name         = "*.${var.domain}."
  type         = "A"
  ttl          = 300
  managed_zone = data.google_dns_managed_zone.zone.name
  rrdatas      = [google_compute_address.static.address]
}
