/**
 * Running things and the state they need: deployments in the cluster, dev runs in
 * a pod, and the cluster secrets and object store both read from.
 */

import { call, callStream, enc, encKey } from "./http";
import type {
  Deployment,
  DeploymentInput,
  DeployOptions,
  EnvBindingInput,
} from "@/app/model/orchestrator";
import type { ClusterSecret } from "@/app/model/secrets";
import type { DevRun, EnsuredDevRun } from "@/app/model/devruns";
import type { ObjectEntry, ObjectValue } from "@/app/model/objects";
import type { ActionResult } from "@octo/http";

// --- Deployments ----------------------------------------------------------

export function listDeployments(
  integrationId: string,
): Promise<ActionResult<Deployment[]>> {
  return call<Deployment[]>(
    "GET",
    `/integrations/${enc(integrationId)}/deployments`,
  );
}

/**
 * One deployment on its own, by id. The list endpoint is per-integration, and a
 * caller holding only a deployment id has no integration to list.
 */
export function getDeployment(id: string): Promise<ActionResult<Deployment>> {
  return call<Deployment>("GET", `/deployments/${enc(id)}`);
}

export function getDeployOptions(
  integrationId: string,
  opts: { slug?: string; expose?: "external"; snapshotId?: string } = {},
): Promise<ActionResult<DeployOptions>> {
  const qs = new URLSearchParams();
  if (opts.slug) qs.set("slug", opts.slug);
  if (opts.expose) qs.set("expose", opts.expose);
  if (opts.snapshotId) qs.set("snapshotId", opts.snapshotId);
  const query = qs.toString();
  return call<DeployOptions>(
    "GET",
    `/integrations/${enc(integrationId)}/deployments/options${
      query ? `?${query}` : ""
    }`,
  );
}

export function createDeployment(
  integrationId: string,
  input: DeploymentInput,
): Promise<ActionResult<Deployment>> {
  return call<Deployment>(
    "POST",
    `/integrations/${enc(integrationId)}/deployments`,
    input,
  );
}

export function rolloutDeployment(
  id: string,
  snapshotId: string,
  env?: Record<string, EnvBindingInput>,
  tracing?: boolean,
): Promise<ActionResult<Deployment>> {
  return call<Deployment>("POST", `/deployments/${enc(id)}/rollout`, {
    snapshotId,
    ...(env ? { env } : {}),
    // Sent only when the caller has an opinion: the orchestrator reads an absent
    // tracing field as "leave this deployment's setting alone", which is what a
    // plain version bump wants.
    ...(tracing === undefined ? {} : { tracing }),
  });
}

export function scaleDeployment(
  id: string,
  replicas: number,
): Promise<ActionResult<Deployment>> {
  return call<Deployment>("PATCH", `/deployments/${enc(id)}`, { replicas });
}

export function deleteDeployment(id: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/deployments/${enc(id)}`);
}

// --- Dev runs -------------------------------------------------------------
// The editor's Run, executed as a pod the orchestrator owns rather than as a child of
// whichever platform replica answered: the BFF holds no run state, so any replica can
// serve any of them.
//
// Every call states whose behalf it acts on. The orchestrator has no session — this BFF
// does, and has already authenticated the caller — so the user id travels as a scope. It
// is a query parameter rather than a path segment because a dev run is addressed by its
// own derived id; the user narrows which runs are reachable, and an operation on
// somebody else's simply is not found.

/**
 * Start a dev run for (userId, integrationId), or attach to the one already running that
 * integration. Idempotent: the orchestrator derives the workload's name from the same
 * pair, so two tabs racing on Run both land on one pod and `created` says which of them
 * made it.
 */
export function ensureDevRun(
  userId: string,
  integrationId: string,
): Promise<ActionResult<EnsuredDevRun>> {
  return call<EnsuredDevRun>("POST", "/devruns", { userId, integrationId });
}

/**
 * The user's live dev runs, narrowed to one integration when given.
 *
 * This is also how "is anything running for me here?" is answered — there is no stored
 * row to consult, so an empty list is the complete answer, and a non-empty one carries
 * the run's live phase and address. One label lookup against the orchestrator's informer
 * cache, so it is cheap enough to call often.
 */
export function listDevRuns(
  userId: string,
  integrationId?: string,
): Promise<ActionResult<DevRun[]>> {
  const qs = new URLSearchParams({ userId });
  if (integrationId) qs.set("integrationId", integrationId);
  return call<DevRun[]>("GET", `/devruns?${qs.toString()}`);
}

/**
 * Tell a dev run to pick up the integration's stored state now.
 *
 * Not the per-edit trigger — a save reaches the run from the orchestrator's own write
 * path. This is the explicit "reload now" a user asks for directly.
 */
export function reloadDevRun(
  userId: string,
  id: string,
): Promise<ActionResult<void>> {
  return call<void>(
    "POST",
    `/devruns/${enc(id)}/reload?userId=${enc(userId)}`,
  );
}

/** Tear a dev run down. There is nothing else to clean up: see app/model/devruns.ts. */
export function deleteDevRun(
  userId: string,
  id: string,
): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/devruns/${enc(id)}?userId=${enc(userId)}`);
}

/**
 * Open the dev run's runtime logs: plain text, one line per line.
 *
 * With `follow` there is no end, so it cannot be read as a document — a JSON client
 * would buffer it forever. Without `follow` it is bounded and ends on its own. `tail`
 * bounds the history either way, because a run that has been up for an hour should not
 * send the hour first.
 */
export function openDevRunLogs(
  userId: string,
  id: string,
  opts: { tail: number; follow: boolean; signal?: AbortSignal },
): Promise<ActionResult<ReadableStream<Uint8Array>>> {
  const qs = new URLSearchParams({ userId, tail: String(opts.tail) });
  if (opts.follow) qs.set("follow", "1");
  return callStream("GET", `/devruns/${enc(id)}/logs?${qs.toString()}`, opts.signal);
}

// --- Secrets --------------------------------------------------------------

export function listSecrets(): Promise<ActionResult<ClusterSecret[]>> {
  return call<ClusterSecret[]>("GET", "/secrets");
}

export function setSecret(
  name: string,
  value: string,
): Promise<ActionResult<ClusterSecret>> {
  return call<ClusterSecret>("PUT", `/secrets/${enc(name)}`, { value });
}

export function deleteSecret(
  name: string,
  force: boolean,
): Promise<ActionResult<void>> {
  return call<void>(
    "DELETE",
    `/secrets/${enc(name)}${force ? "?force=true" : ""}`,
  );
}

// --- Objects --------------------------------------------------------------
// The deployment-scoped object browser: a JSON facade over the orchestrator's KV
// store, fixed server-side to the user-facing "user" namespace. The list and write
// endpoints wrap their payload ({ items } / { version }); we unwrap to the bare
// shape the model expects.

/** The `?namespace=` suffix when a non-default namespace is named, else empty. */
const nsQuery = (namespace?: string): string =>
  namespace ? `?namespace=${enc(namespace)}` : "";

export async function listNamespaces(
  deploymentId: string,
): Promise<ActionResult<string[]>> {
  const res = await call<{ items: string[] }>(
    "GET",
    `/deployments/${enc(deploymentId)}/namespaces`,
  );
  return res.ok ? { ok: true, data: res.data.items } : res;
}

export async function listObjects(
  deploymentId: string,
  namespace?: string,
): Promise<ActionResult<ObjectEntry[]>> {
  const res = await call<{ items: ObjectEntry[] }>(
    "GET",
    `/deployments/${enc(deploymentId)}/objects${nsQuery(namespace)}`,
  );
  return res.ok ? { ok: true, data: res.data.items } : res;
}

export function getObject(
  deploymentId: string,
  key: string,
  namespace?: string,
): Promise<ActionResult<ObjectValue>> {
  return call<ObjectValue>(
    "GET",
    `/deployments/${enc(deploymentId)}/objects/${encKey(key)}${nsQuery(namespace)}`,
  );
}

export async function setObject(
  deploymentId: string,
  key: string,
  value: string,
  version: number,
  encoding: "utf8" | "base64",
  namespace?: string,
): Promise<ActionResult<number>> {
  const res = await call<{ version: number }>(
    "PUT",
    `/deployments/${enc(deploymentId)}/objects/${encKey(key)}${nsQuery(namespace)}`,
    { value, encoding, version },
  );
  return res.ok ? { ok: true, data: res.data.version } : res;
}

export function deleteObject(
  deploymentId: string,
  key: string,
  version: number,
  namespace?: string,
): Promise<ActionResult<void>> {
  // version is the primary query param; the namespace (when set) is appended.
  const ns = namespace ? `&namespace=${enc(namespace)}` : "";
  return call<void>(
    "DELETE",
    `/deployments/${enc(deploymentId)}/objects/${encKey(key)}?version=${version}${ns}`,
  );
}
