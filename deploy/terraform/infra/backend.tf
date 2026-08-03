# Remote state, versioned, shared bucket — one prefix per root. A local
# terraform.tfstate in the working tree is one `rm -rf` away from orphaning a VM, a
# static IP and a data disk with nothing left able to destroy them.
#
# The bucket is supplied at init time (-backend-config=../backend.hcl) because a
# backend block cannot reference variables; the prefix is a literal because it
# identifies this root and must not be shared with another.
terraform {
  backend "gcs" {
    prefix = "infra"
  }
}
