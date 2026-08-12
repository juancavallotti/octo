# How the platform runs an integration

The lifecycle behind the API you call through `octo_api`. Read this before
diagnosing a deployment or explaining why an edit has not taken effect.

## Integration, version tag, deployment

Three separate things, and most confusion comes from conflating them:

- **Integration** — the working copy. `PUT /integrations/{id}` changes it. Editing
  it does **not** change anything that is running.
- **Version tag (snapshot)** — a frozen copy of the definition *and* the resources,
  taken by `POST /integrations/{id}/snapshots` with a tag. Immutable, unique per
  integration.
- **Deployment** — a Kubernetes workload running one tag.
  `POST /integrations/{id}/deployments` creates it.

So "I changed the flow but the deployment still does the old thing" is the system
working: the deployment is running a tag, and a tag never changes. Tag the new
version and roll out.

## Deploying

`POST /integrations/{id}/deployments` with settings:

| Setting | Meaning |
| --- | --- |
| `snapshotId` | The tag to run. Required in production. |
| `replicas` | Defaults to 1. |
| `slug` | The internal address label; empty asks for one to be allocated. |
| `expose` | `external` publishes a public endpoint; empty is internal only. |
| `subdomain` | The external host label. |
| `env` | Binds declared variables: `{"value": "..."}` or `{"secret": "name"}`. |
| `tracing` | Off by default. |

A binding is a secret reference when `secret` is non-empty, and only the **name** is
stored — never the value. Put credentials in a cluster secret
(`PUT /secrets/{name}`) and bind by name.

`HTTP_PORT` and `HTTP_HOST` are orchestrator-managed and cannot be bound.

A deployment is **networked** — it gets a Service and a stable internal address —
when the definition declares `HTTP_PORT` with a numeric default.

`GET /integrations/{id}/deployments/options` reports what a deploy would need: the
variables the tag declares, whether an address is free, whether external exposure is
available. Prefer it over discovering the gaps through failed deploys.

## Changing a live deployment

| Change | How |
| --- | --- |
| Version, env bindings, or tracing | `POST /deployments/{id}/rollout` |
| Replica count | `PATCH /deployments/{id}` |
| Remove it | `DELETE /deployments/{id}` |

Rollout is a rolling update in place: the id, address, scale and exposure survive.
Omitting `env` or `tracing` keeps its existing value, so a plain version bump runs
the new tag on the settings the deployment already had.

**Tracing only changes on a rollout**, because the runtime reads it at startup — so
turning it on replaces the pods.

A tag that adds or removes the integration's HTTP source is refused: that changes the
Service topology, which a rolling update cannot express. Undeploy and redeploy.

## When a deployment will not start

Work through it in this order, because each step rules out the one below:

1. **`GET /deployments/{id}`** — the status carries a `reason` and per-pod detail
   including restart counts. A crash loop shows up here as restarts climbing.
2. **A required variable with no binding** is refused *before* the pod is created,
   with the names in the error. If the deploy itself failed, this is usually why.
3. **`GET /deployments/{id}/pods/{pod}/logs`** — the runtime logs a precise error
   when a config will not load: an undeclared `${NAME}`, an unknown connector type, a
   leaf block declaring composite slots, a connector whose required setting is empty.
4. **A secret binding that names a secret that does not exist** fails the pod at
   start, not the deploy.

## Resources

Env files and templates belong to the integration
(`POST /integrations/{id}/resources`, kind `env` or `template`). Names are relative
paths and may contain slashes. A snapshot freezes a copy of them, and a running pod
reads them from its snapshot — so adding a resource to the integration does not
reach a deployment until a new tag is rolled out.

## Storage a deployment has

- **KV / objects**, scoped to the deployment and served by the orchestrator:
  `GET|PUT|DELETE /deployments/{id}/objects/{key}` and
  `GET /deployments/{id}/objects` to list. The `object-*` blocks use this. All
  replicas share it; other deployments cannot see it.
- **Queues and topics** over the broker, scoped to the deployment.
- **Cluster secrets**, shared across the install, referenced by name.

## Dev runs

`POST /devruns` starts a short-lived pod running an integration's **working copy** —
no tag — so the editor can execute unsaved work. That is the one path where an edit
takes effect without a tag, and it is not how anything runs in production.
