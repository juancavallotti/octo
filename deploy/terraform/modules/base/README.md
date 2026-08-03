# `base`

Single-VM infrastructure for the k3s deployment.

Service account, static IP, firewall rules (80/443, 22, optional 6443), the GCE
instance, the **Postgres data disk** (with a daily snapshot schedule), and the DNS
A + wildcard records.

Workload-agnostic: the caller supplies the startup script and any instance
metadata, so nothing about octo is baked in here.

The data disk is the part worth understanding. It is a resource of its own rather
than a `boot_disk` — a non-boot `attached_disk` is never auto-deleted with the
instance, so the database survives the VM being replaced. Formatting and mounting
it is the caller's startup script's job; this module only creates and attaches the
device.

**Used by:** infra

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
