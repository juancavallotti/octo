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
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# Both point at the k3s cluster via the kubeconfig fetched by `task deploy:kubeconfig`
# (local.kubeconfig defaults to ../k3s/kubeconfig.yaml when var.kubeconfig is unset).
provider "kubernetes" {
  config_path = local.kubeconfig
}

provider "helm" {
  kubernetes {
    config_path = local.kubeconfig
  }
}
