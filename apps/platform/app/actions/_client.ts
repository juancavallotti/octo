/**
 * The high-level orchestrator client lib — the middle layer between the server
 * actions and the `fetch` abstraction (`_http.ts`):
 *
 *     serverAction (auth) → this client (listFolders(), …) → requestJson() → fetch
 *
 * It is a typed, domain-oriented API: one named function per orchestrator
 * operation. It deliberately exposes NO HTTP verbs — paths, methods, JSON
 * encoding, and the server-only `ORCHESTRATOR_URL` are all internal. It is also
 * auth-agnostic; authorization is applied by the calling action (`_auth.ts`).
 *
 * Every function returns a discriminated {@link ActionResult} (server actions
 * can't throw readable errors in production); the model layer unwraps it.
 */

import { requestJson, requestOk, type ActionResult } from "@octo/http";
import type {
  Deployment,
  DeploymentInput,
  DeployOptions,
  Folder,
  Integration,
  IntegrationInput,
  Resource,
  Snapshot,
  SnapshotResource,
  User,
} from "@/app/model/orchestrator";
import type {
  ApiKey,
  CreatedApiKey,
  VerifiedApiKey,
} from "@/app/model/apikeys";
import type { ClusterSecret } from "@/app/model/secrets";
import type { ObjectEntry, ObjectValue } from "@/app/model/objects";

export type { ActionResult } from "@octo/http";

const enc = encodeURIComponent;

/**
 * Encode an object key for the `{key...}` path wildcard: keys may contain slashes
 * (which must stay real path separators), so encode each segment but keep the
 * slashes between them.
 */
const encKey = (key: string): string => key.split("/").map(enc).join("/");

/** The orchestrator base URL with any trailing slash trimmed, or "" when unset. */
function baseUrl(): string {
  return (process.env.ORCHESTRATOR_URL ?? "").replace(/\/+$/, "");
}

/**
 * Issue one orchestrator request. Internal: the public API is the named domain
 * functions below, never a verb. Returns an error result when the orchestrator is
 * unconfigured (mirroring the route proxy's 503).
 */
function call<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ActionResult<T>> {
  const base = baseUrl();
  if (!base) {
    return Promise.resolve({
      ok: false,
      error: "orchestrator not configured (ORCHESTRATOR_URL unset)",
    });
  }
  return requestJson<T>(method, `${base}${path}`, body);
}

// --- Health ---------------------------------------------------------------

/**
 * Whether the orchestrator is configured and answers its health check. Uses a
 * body-less probe (the `/healthz` response is plain text "ok", not JSON), so it
 * doesn't go through the JSON client.
 */
export function checkHealth(): Promise<boolean> {
  const base = baseUrl();
  if (!base) return Promise.resolve(false);
  return requestOk("GET", `${base}/healthz`);
}

// --- Users ----------------------------------------------------------------

/**
 * Provision (or refresh) the user identified by the OIDC subject, returning the
 * row with its durable id. Called from the auth layer on sign-in; idempotent, so
 * later logins just sync email/name.
 */
export function bootstrapUser(
  subject: string,
  email: string,
  name: string,
): Promise<ActionResult<User>> {
  return call<User>("POST", "/users/bootstrap", { subject, email, name });
}

// --- API keys -------------------------------------------------------------
// Per-user bearer tokens, nested under their owner so the owner id never travels
// in a header. createApiKey is the only call that returns the plaintext token.

export function listApiKeys(userId: string): Promise<ActionResult<ApiKey[]>> {
  return call<ApiKey[]>("GET", `/users/${enc(userId)}/apikeys`);
}

export function createApiKey(
  userId: string,
  name: string,
  ttlSeconds: number,
): Promise<ActionResult<CreatedApiKey>> {
  return call<CreatedApiKey>("POST", `/users/${enc(userId)}/apikeys`, {
    name,
    ttlSeconds,
  });
}

export function deleteApiKey(
  userId: string,
  id: string,
): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/users/${enc(userId)}/apikeys/${enc(id)}`);
}

/** Resolve a presented bearer token to its owner, or an error result if it is
 * unknown, revoked, or expired. Used by the `/mcp` route to authenticate. */
export function verifyApiKey(
  token: string,
): Promise<ActionResult<VerifiedApiKey>> {
  return call<VerifiedApiKey>("POST", "/apikeys/verify", { token });
}

// --- Integrations ---------------------------------------------------------

export function listIntegrations(): Promise<ActionResult<Integration[]>> {
  return call<Integration[]>("GET", "/integrations");
}

export function getIntegration(
  id: string,
): Promise<ActionResult<Integration>> {
  return call<Integration>("GET", `/integrations/${enc(id)}`);
}

export function createIntegration(
  input: IntegrationInput,
  actorId?: string,
): Promise<ActionResult<Integration>> {
  return call<Integration>("POST", "/integrations", { ...input, actorId });
}

export function updateIntegration(
  id: string,
  input: IntegrationInput,
  actorId?: string,
): Promise<ActionResult<Integration>> {
  return call<Integration>("PUT", `/integrations/${enc(id)}`, {
    ...input,
    actorId,
  });
}

export function deleteIntegration(id: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/integrations/${enc(id)}`);
}

// --- Folders --------------------------------------------------------------

export function listFolders(): Promise<ActionResult<Folder[]>> {
  return call<Folder[]>("GET", "/folders");
}

export function createFolder(
  name: string,
  parentId: string | null,
): Promise<ActionResult<Folder>> {
  return call<Folder>("POST", "/folders", { name, parentId });
}

export function renameFolder(
  id: string,
  name: string,
  parentId: string | null,
): Promise<ActionResult<Folder>> {
  return call<Folder>("PUT", `/folders/${enc(id)}`, { name, parentId });
}

export function deleteFolder(id: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/folders/${enc(id)}`);
}

export function reorderFolders(
  parentId: string | null,
  folderIds: string[],
): Promise<ActionResult<void>> {
  return call<void>("PUT", "/folders/reorder", { parentId, folderIds });
}

export function listFolderIntegrations(
  folderId: string,
): Promise<ActionResult<Integration[]>> {
  return call<Integration[]>("GET", `/folders/${enc(folderId)}/integrations`);
}

export function assignIntegration(
  folderId: string,
  integrationId: string,
): Promise<ActionResult<void>> {
  return call<void>(
    "PUT",
    `/folders/${enc(folderId)}/integrations/${enc(integrationId)}`,
  );
}

export function unassignIntegration(
  folderId: string,
  integrationId: string,
): Promise<ActionResult<void>> {
  return call<void>(
    "DELETE",
    `/folders/${enc(folderId)}/integrations/${enc(integrationId)}`,
  );
}

export function reorderFolderIntegrations(
  folderId: string,
  integrationIds: string[],
): Promise<ActionResult<void>> {
  return call<void>("PUT", `/folders/${enc(folderId)}/integration-order`, {
    integrationIds,
  });
}

// --- Snapshots (version tags) ---------------------------------------------

export function listSnapshots(
  integrationId: string,
): Promise<ActionResult<Snapshot[]>> {
  return call<Snapshot[]>("GET", `/integrations/${enc(integrationId)}/snapshots`);
}

export function createSnapshot(
  integrationId: string,
  tag: string,
): Promise<ActionResult<Snapshot>> {
  return call<Snapshot>("POST", `/integrations/${enc(integrationId)}/snapshots`, {
    tag,
  });
}

export function deleteSnapshot(id: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/snapshots/${enc(id)}`);
}

/** The resources frozen alongside a tag's definition (metadata only; content is
 * served separately by the runtime's loader). */
export function listSnapshotResources(
  snapshotId: string,
): Promise<ActionResult<SnapshotResource[]>> {
  return call<SnapshotResource[]>(
    "GET",
    `/snapshots/${enc(snapshotId)}/resources`,
  );
}

// --- Resources ------------------------------------------------------------
// Every route is nested under the owning integration; the resource id alone is
// not a valid address.

export function listResources(
  integrationId: string,
): Promise<ActionResult<Resource[]>> {
  return call<Resource[]>("GET", `/integrations/${enc(integrationId)}/resources`);
}

export function getResource(
  integrationId: string,
  id: string,
): Promise<ActionResult<Resource>> {
  return call<Resource>(
    "GET",
    `/integrations/${enc(integrationId)}/resources/${enc(id)}`,
  );
}

export function createResource(
  integrationId: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  return call<Resource>(
    "POST",
    `/integrations/${enc(integrationId)}/resources`,
    { kind, name, content },
  );
}

export function deleteResource(
  integrationId: string,
  id: string,
): Promise<ActionResult<void>> {
  return call<void>(
    "DELETE",
    `/integrations/${enc(integrationId)}/resources/${enc(id)}`,
  );
}

export function updateResource(
  integrationId: string,
  id: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  return call<Resource>(
    "PUT",
    `/integrations/${enc(integrationId)}/resources/${enc(id)}`,
    { kind, name, content },
  );
}

/**
 * Create or replace a resource by its (path-like) name. Resource names are
 * addressed by the orchestrator's opaque id, not by name, so this lists the
 * integration's resources, and updates the match by id or creates a new one. Used
 * for name-keyed resources like `.env.dev` that the editor owns without tracking
 * their id.
 */
export async function upsertResourceByName(
  integrationId: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  const existing = await listResources(integrationId);
  if (!existing.ok) return existing;
  const match = existing.data.find((r) => r.name === name);
  return match
    ? updateResource(integrationId, match.id, kind, name, content)
    : createResource(integrationId, kind, name, content);
}

// --- Deployments ----------------------------------------------------------

export function listDeployments(
  integrationId: string,
): Promise<ActionResult<Deployment[]>> {
  return call<Deployment[]>(
    "GET",
    `/integrations/${enc(integrationId)}/deployments`,
  );
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
): Promise<ActionResult<Deployment>> {
  return call<Deployment>("POST", `/deployments/${enc(id)}/rollout`, {
    snapshotId,
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
