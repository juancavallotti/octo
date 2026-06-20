# Deploying octo to GCP (single-node k3s)

Terraform to run octo on one GCP VM with single-node k3s, Traefik, and
cert-manager (free Let's Encrypt TLS). The editor is served at your domain; the
orchestrator and Postgres stay internal. Per-integration subdomains
(`*.{domain}`) are wired for Stage 2.

The **octo release is owned by Terraform** (the Helm provider): the VM only
bootstraps the cluster, and the chart is installed/upgraded by applying the
`release/` root. On a version tag the Cloud Build job runs that release for you
(`_DEPLOY=true`); you can also run it by hand with `task deploy`.

> This README is the command quick-reference. For the full guide — architecture,
> configuration reference, integration endpoints, operations and troubleshooting —
> see [docs/deployment.md](../../docs/deployment.md).

## Layout

| Dir | Purpose | Apply |
|---|---|---|
| `modules/registry` | Artifact Registry repo (Docker + OCI Helm chart). | (module) |
| `modules/base` | Reusable single-VM infra: SA, secret, static IP, firewall (80/443/22 + optional 6443), instance, DNS A + wildcard records. | (module) |
| `modules/cloudbuild` | Cloud Build trigger + Artifact Registry writer IAM + (optional) deploy-step IAM. | (module) |
| `modules/helm-release` | The octo `helm_release` from the Artifact Registry OCI chart. | (module) |
| `infra/` | **Combined one-time root**: registry + the VM/k3s bootstrap + (optional) the Cloud Build trigger. | once |
| `release/` | The Helm release (Terraform owns it; Cloud Build or `task deploy` applies it). | per deploy/upgrade |

Region defaults to **us-west1** (Oregon). There is **one tfvars file** — `octo.tfvars`
(gitignored) — read by both roots; per-deploy values (`image_tag`, `chart_version`)
come from the command line. `release/` state is in GCS (bucket created by
`task state:bucket`) so Cloud Build and your laptop share it; the generated Postgres
password and the fetched kubeconfig are gitignored.

## One-time setup

```sh
gcloud auth application-default login

# 0. Fill in the one tfvars file (project_id, domain, dns_managed_zone).
cd deploy/terraform && cp octo.tfvars.example octo.tfvars

# 1. Remote state bucket for the release root (run once).
task state:bucket PROJECT=<your-project>

# 2. Everything one-time: registry + VM + k3s bootstrap. Leave enable_cloudbuild unset
#    for now (the trigger needs the GitHub App connected first).
task infra:apply
terraform -chdir=infra output      # static_ip, url, kube_api_endpoint

# 3. (Optional) Cloud Build automation. Connect the GitHub repo once in the console
#    (Cloud Build → Triggers → Connect repository), set enable_cloudbuild=true in
#    octo.tfvars, and re-run:
task infra:apply
```

## Publish images + chart

- **Automated:** push a version tag (release-please publishes `vX.Y.Z`) — the Cloud
  Build trigger builds all four images and the chart, pushes them to Artifact Registry,
  and (with `_DEPLOY=true`, the default when `cloudbuild_auto_deploy` is on) rolls the
  cluster.
- **Manual:** from the repo root, with `IMAGE_BASE` = `<region>-docker.pkg.dev/<project>/octo`:
  ```sh
  gcloud auth configure-docker us-west1-docker.pkg.dev
  helm registry login us-west1-docker.pkg.dev -u oauth2accesstoken -p "$(gcloud auth print-access-token)"
  task images:push IMAGE_BASE=$IMAGE_BASE TAG=v0.1.1
  task helm:push   IMAGE_BASE=$IMAGE_BASE
  ```

## Deploy / roll upgrades

On a version tag Cloud Build does this automatically. To deploy a published tag by
hand (fetches the kubeconfig, derives the chart version from `helm/Chart.yaml`, applies
the release root):

```sh
task deploy TAG=v0.1.1            # optional: DOMAIN=… INSTANCE=… ZONE=…
```

Each apply pulls the target tag onto the node (fresh token, via `octo-pull` over SSH),
then installs/upgrades the chart. Bumping the tag rewrites the pod templates, so the
Deployments roll automatically; Postgres is untouched when only the tag moves. First TLS
issuance takes a minute after DNS resolves.

Verify:

```sh
curl -I https://<domain>                                   # valid Let's Encrypt cert
gcloud compute ssh octo --zone us-west1-a -- sudo k3s kubectl get pods -n octo-dev
```
