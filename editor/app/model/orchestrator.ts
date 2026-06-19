/**
 * Browser-side client for the orchestrator, talking to the editor's BFF proxy
 * routes under `/api` (never the orchestrator directly — see
 * `app/api/orchestrator/client.ts`). Every call unwraps the orchestrator's
 * `{ error }` envelope on failure, the same convention RunContext uses.
 */

/** A stored integration: a named flow definition (YAML) plus bookkeeping. */
export interface Integration {
  id: string;
  name: string;
  /** The flow definition, as the runtime YAML the editor serializes. */
  definition: string;
  /** RFC3339 timestamp of the last update. */
  lastUpdated: string;
}

/** A folder in the single-membership organization tree. */
export interface Folder {
  id: string;
  parentId: string | null;
  name: string;
  /** Present on the tree returned by `listFolders`; nested children. */
  children?: Folder[];
}

/** Body for creating/updating an integration. */
export interface IntegrationInput {
  name: string;
  definition: string;
}

/** Coarse lifecycle status of a deployment, cached from the live cluster. */
export type DeploymentStatus = "pending" | "running" | "failed";

/** One deployed instance of an integration running as its own workload. */
export interface Deployment {
  id: string;
  integrationId: string;
  /** Display name, captured from the integration at deploy time. */
  name: string;
  /** Cached lifecycle status; refreshed by the orchestrator on read. */
  status: DeploymentStatus;
  /** Desired/served replica count. */
  replicas: number;
  /** In-cluster address other flows use to reach this integration, if any. */
  internalUrl?: string;
  /** Public https URL when the deployment is exposed externally. */
  externalUrl?: string;
  /** RFC3339 timestamp of the last status/state update. */
  lastUpdated: string;
}

/** Per-deployment options sent when deploying an integration. */
export interface DeploymentInput {
  /** Runtime replicas; omitted/<=0 means a single replica. */
  replicas?: number;
  /** "external" publishes a {subdomain}.{baseDomain} endpoint with TLS. */
  expose?: "external";
  /** External host label; defaults to the integration slug when omitted. */
  subdomain?: string;
}

/** Perform a JSON request against a BFF route, unwrapping the `{ error }` envelope. */
async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `request failed (${res.status})`);
  }
  // 204 No Content (delete / folder assignment) carries no body.
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

function jsonBody(data: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  };
}

// --- Integrations ---------------------------------------------------------

export function listIntegrations(): Promise<Integration[]> {
  return request<Integration[]>("/api/integrations");
}

export function getIntegration(id: string): Promise<Integration> {
  return request<Integration>(`/api/integrations/${encodeURIComponent(id)}`);
}

export function createIntegration(
  input: IntegrationInput,
): Promise<Integration> {
  return request<Integration>("/api/integrations", jsonBody(input));
}

export function updateIntegration(
  id: string,
  input: IntegrationInput,
): Promise<Integration> {
  return request<Integration>(`/api/integrations/${encodeURIComponent(id)}`, {
    ...jsonBody(input),
    method: "PUT",
  });
}

export function deleteIntegration(id: string): Promise<void> {
  return request<void>(`/api/integrations/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

// --- Deployments ----------------------------------------------------------

/** List the deployments of an integration (status refreshed server-side on read). */
export function listDeployments(integrationId: string): Promise<Deployment[]> {
  return request<Deployment[]>(
    `/api/integrations/${encodeURIComponent(integrationId)}/deployments`,
  );
}

/** Deploy an integration as a new workload, optionally exposed externally. */
export function createDeployment(
  integrationId: string,
  input: DeploymentInput = {},
): Promise<Deployment> {
  return request<Deployment>(
    `/api/integrations/${encodeURIComponent(integrationId)}/deployments`,
    jsonBody(input),
  );
}

/** Undeploy a deployment, removing its workload. */
export function deleteDeployment(id: string): Promise<void> {
  return request<void>(`/api/deployments/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

// --- Folders --------------------------------------------------------------

export function listFolders(): Promise<Folder[]> {
  return request<Folder[]>("/api/folders");
}

export function createFolder(
  name: string,
  parentId: string | null = null,
): Promise<Folder> {
  return request<Folder>("/api/folders", jsonBody({ name, parentId }));
}

export function renameFolder(
  id: string,
  name: string,
  parentId: string | null,
): Promise<Folder> {
  return request<Folder>(`/api/folders/${encodeURIComponent(id)}`, {
    ...jsonBody({ name, parentId }),
    method: "PUT",
  });
}

export function deleteFolder(id: string): Promise<void> {
  return request<void>(`/api/folders/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function listFolderIntegrations(
  folderId: string,
): Promise<Integration[]> {
  return request<Integration[]>(
    `/api/folders/${encodeURIComponent(folderId)}/integrations`,
  );
}

/** Add an integration to a folder (single-membership: replaces any prior folder). */
export function assignIntegration(
  folderId: string,
  integrationId: string,
): Promise<void> {
  return request<void>(
    `/api/folders/${encodeURIComponent(folderId)}/integrations/${encodeURIComponent(integrationId)}`,
    { method: "PUT" },
  );
}

/** Remove an integration from a folder. */
export function unassignIntegration(
  folderId: string,
  integrationId: string,
): Promise<void> {
  return request<void>(
    `/api/folders/${encodeURIComponent(folderId)}/integrations/${encodeURIComponent(integrationId)}`,
    { method: "DELETE" },
  );
}

/** Collect every folder id in the tree, depth-first. */
function folderIds(folders: Folder[]): string[] {
  return folders.flatMap((f) => [f.id, ...folderIds(f.children ?? [])]);
}

/**
 * Find which folder an integration belongs to, or null when unfiled. Integrations
 * are single-membership but the integration record doesn't name its folder, so we
 * scan folder memberships. Used when opening an integration by its bookmarkable
 * URL, where the folder isn't otherwise known.
 */
export async function findIntegrationFolderId(
  integrationId: string,
): Promise<string | null> {
  const ids = folderIds(await listFolders());
  const matches = await Promise.all(
    ids.map((id) =>
      listFolderIntegrations(id).then((items) =>
        items.some((i) => i.id === integrationId) ? id : null,
      ),
    ),
  );
  return matches.find((id): id is string => id !== null) ?? null;
}
