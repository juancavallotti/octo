# Deploying octo to GCP (single-node k3s)

Terraform to run octo on one GCP VM with single-node k3s, Traefik, and
cert-manager (free Let's Encrypt TLS). The editor is served at your domain; the
orchestrator and Postgres stay internal. Per-integration subdomains
(`*.{domain}`) are wired for Stage 2.

The **octo release is owned by Terraform** (the Helm provider): the VM only
bootstraps the cluster, and the chart is installed/upgraded by applying the
`release/` root after images are published.

> This README is the command quick-reference. For the full guide — architecture,
> configuration reference, integration endpoints, operations and troubleshooting —
> see [docs/deployment.md](../../docs/deployment.md).

## Layout

| Dir | Purpose | Apply |
|---|---|---|
| `modules/base` | Reusable single-VM infra: SA, secret, static IP, firewall (80/443/22 + optional 6443), instance, DNS A + wildcard records. | (module) |
| `modules/cloudbuild` | Cloud Build trigger + Artifact Registry writer IAM. | (module) |
| `modules/helm-release` | The octo `helm_release` from the Artifact Registry OCI chart. | (module) |
| `registry/` | Artifact Registry repo (Docker + OCI Helm chart). | once |
| `cloudbuild/` | One-time Cloud Build setup; a version tag auto-builds & pushes images + chart. | once |
| `k3s/` | The VM + k3s bootstrap (k3s, cert-manager, ClusterIssuer, octo-pull helper). | once per cluster |
| `release/` | The Helm release (Terraform owns it). | per deploy/upgrade |

Region defaults to **us-west1** (Oregon). State holds the generated Postgres
password and the kubeconfig is fetched locally — both are gitignored.

## One-time setup

```sh
gcloud auth application-default login

# 1. Artifact Registry (images + OCI chart).
cd registry && cp terraform.tfvars.example terraform.tfvars   # set project_id
terraform init && terraform apply

# 2. (Optional) Cloud Build automation. First connect the GitHub repo to Cloud
#    Build once in the console (Cloud Build → Triggers → Connect repository), then:
cd ../cloudbuild && cp terraform.tfvars.example terraform.tfvars
terraform init && terraform apply

# 3. The VM + cluster bootstrap.
cd ../k3s && cp terraform.tfvars.example terraform.tfvars   # project_id, dns_managed_zone, domain
terraform init && terraform apply
terraform output      # static_ip, url, kube_api_endpoint

# 4. Fetch the cluster kubeconfig for the release root (server -> https://{domain}:6443).
cd ../../.. && task deploy:kubeconfig DOMAIN=octo.juancavallotti.com
```

## Publish images + chart

- **Automated:** push a version tag (release-please publishes `vX.Y.Z`) — the
  Cloud Build trigger builds all four images and the chart and pushes them to
  Artifact Registry (tagged with the git tag and `latest`).
- **Manual:** from the repo root, with `IMAGE_BASE` = `<region>-docker.pkg.dev/<project>/octo`:
  ```sh
  gcloud auth configure-docker us-west1-docker.pkg.dev
  helm registry login us-west1-docker.pkg.dev -u oauth2accesstoken -p "$(gcloud auth print-access-token)"
  task images:push IMAGE_BASE=$IMAGE_BASE TAG=v0.1.1
  task helm:push   IMAGE_BASE=$IMAGE_BASE
  ```

## Deploy / roll upgrades

Apply the release root right after publishing — set `image_tag` (and
`chart_version` if the chart changed):

```sh
cd deploy/terraform/release && cp terraform.tfvars.example terraform.tfvars
terraform init
terraform apply -var image_tag=v0.1.1 -var chart_version=0.1.1
```

Each apply pulls the target tag onto the node (fresh token, via `octo-pull` over
SSH), then installs/upgrades the chart. Bumping `image_tag` rewrites the pod
templates, so the Deployments roll automatically; Postgres is untouched when only
the tag moves. First TLS issuance takes a minute after DNS resolves.

Verify:

```sh
curl -I https://<domain>                                   # valid Let's Encrypt cert
gcloud compute ssh octo --zone us-west1-a -- sudo k3s kubectl get pods -n octo-dev
```
