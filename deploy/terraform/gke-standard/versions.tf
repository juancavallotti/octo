terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.23"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# The cluster is authenticated with the caller's own gcloud access token, resolved
# at apply time — no kubeconfig file is written, fetched or left on disk. Terraform
# defers reading these until after the cluster exists, so cluster and workloads can
# be created in a single apply.
provider "kubernetes" {
  host                   = "https://${module.octo_gke.cluster_endpoint}"
  cluster_ca_certificate = base64decode(module.octo_gke.cluster_ca_certificate)
  token                  = data.google_client_config.default.access_token
}

provider "helm" {
  kubernetes {
    host                   = "https://${module.octo_gke.cluster_endpoint}"
    cluster_ca_certificate = base64decode(module.octo_gke.cluster_ca_certificate)
    token                  = data.google_client_config.default.access_token
  }
}
