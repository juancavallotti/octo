# Shared single-VM infrastructure: APIs, service account, static IP, firewall, the
# instance itself, and the DNS A record(s). The workload (what actually runs on the
# VM) is injected via var.startup_script + var.metadata.

resource "google_project_service" "apis" {
  for_each = toset([
    "compute.googleapis.com",
    "dns.googleapis.com",
  ])
  service            = each.value
  disable_on_destroy = false
}

resource "google_service_account" "vm" {
  account_id   = "${var.instance_name}-vm"
  display_name = "octo VM (${var.instance_name})"
}

# cert-manager (running on the node, using the VM SA via ambient credentials)
# solves the DNS-01 challenge for the *.{domain} wildcard cert by writing TXT
# records into the managed zone, so it needs DNS admin on the project.
resource "google_project_iam_member" "vm_dns" {
  count   = var.wildcard_dns ? 1 : 0
  project = var.project_id
  role    = "roles/dns.admin"
  member  = "serviceAccount:${google_service_account.vm.email}"
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

# --- Data disk ---
# The database lives here, not on the boot disk. The boot disk is recreated with the
# instance — a machine-type change, an image bump, a rebuild — and the boot disk is
# also where k3s's local-path provisioner would otherwise put Postgres, so the two
# failure modes compound. This is a resource of its own, and a non-boot attached_disk
# is never auto-deleted by the provider, so the data outlives the VM.
#
# Deliberately no `prevent_destroy`: this environment still gets torn down and
# rebuilt on purpose, and a lifecycle block that cannot be toggled by a variable
# turns that into an edit-the-module dance. The snapshot schedule below is what
# makes an accidental destroy recoverable. Uncomment when it goes long-lived:
#
#   lifecycle { prevent_destroy = true }
resource "google_compute_disk" "data" {
  name = "${var.instance_name}-data"
  type = var.data_disk_type
  zone = var.zone
  size = var.data_disk_size_gb

  labels = {
    component = "octo-data"
  }

  depends_on = [google_project_service.apis]
}

# A data disk with no backups is not hardened. KEEP_AUTO_SNAPSHOTS means the
# snapshots survive the disk itself, which is the case that matters.
resource "google_compute_resource_policy" "data_snapshot" {
  count  = var.data_disk_snapshot_retention_days > 0 ? 1 : 0
  name   = "${var.instance_name}-data-snapshot"
  region = var.region

  # Same dependency the disk and the VM carry. Nothing else here references the
  # policy's inputs, so on a fresh project Terraform is free to schedule it while
  # compute.googleapis.com is still enabling; the attachment below then has no
  # policy to attach and the apply fails on the second resource rather than the
  # first.
  depends_on = [google_project_service.apis]

  snapshot_schedule_policy {
    schedule {
      daily_schedule {
        days_in_cycle = 1
        start_time    = var.data_disk_snapshot_start_time
      }
    }
    retention_policy {
      max_retention_days    = var.data_disk_snapshot_retention_days
      on_source_disk_delete = "KEEP_AUTO_SNAPSHOTS"
    }
    snapshot_properties {
      storage_locations = [var.region]
    }
  }
}

resource "google_compute_disk_resource_policy_attachment" "data_snapshot" {
  count = var.data_disk_snapshot_retention_days > 0 ? 1 : 0
  name  = google_compute_resource_policy.data_snapshot[0].name
  disk  = google_compute_disk.data.name
  zone  = var.zone
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

  # Surfaces to the guest as /dev/disk/by-id/google-{device_name}, which is what
  # the startup script formats and mounts. Attaching to a running instance is an
  # in-place update — no VM replacement.
  attached_disk {
    source      = google_compute_disk.data.id
    device_name = var.data_disk_device_name
    mode        = "READ_WRITE"
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

  depends_on = [google_project_service.apis]
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
# node; cert-manager issues a single *.{domain} wildcard cert via DNS-01 that all
# subdomains share (Stage 2).
resource "google_dns_record_set" "wildcard" {
  count        = var.wildcard_dns ? 1 : 0
  name         = "*.${var.domain}."
  type         = "A"
  ttl          = 300
  managed_zone = data.google_dns_managed_zone.zone.name
  rrdatas      = [google_compute_address.static.address]
}
