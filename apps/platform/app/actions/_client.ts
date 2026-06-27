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

import { requestJson, type ActionResult } from "@octo/http";
import type {
  Deployment,
  DeploymentInput,
  DeployOptions,
  Folder,
  Integration,
  IntegrationInput,
  Snapshot,
} from "@/app/model/orchestrator";

export type { ActionResult } from "@octo/http";

const enc = encodeURIComponent;

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
): Promise<ActionResult<Integration>> {
  return call<Integration>("POST", "/integrations", input);
}

export function updateIntegration(
  id: string,
  input: IntegrationInput,
): Promise<ActionResult<Integration>> {
  return call<Integration>("PUT", `/integrations/${enc(id)}`, input);
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
