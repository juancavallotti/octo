/**
 * Browser-side client for the orchestrator, talking to the editor's BFF proxy
 * routes under `/api` (never the orchestrator directly — see
 * `app/api/orchestrator/client.ts`). Every call unwraps the orchestrator's
 * `{ error }` envelope on failure, the same convention RunContext uses.
 */

import * as deploymentActions from "@/app/actions/deployments";
import * as folderActions from "@/app/actions/folders";
import * as integrationActions from "@/app/actions/integrations";
import * as snapshotActions from "@/app/actions/snapshots";
import { unwrap } from "./bff";

/** A stored integration: a named flow definition (YAML) plus bookkeeping. */
export interface Integration {
  id: string;
  name: string;
  /** The flow definition, as the runtime YAML the editor serializes. */
  definition: string;
  /** RFC3339 timestamp of the last update. */
  lastUpdated: string;
}

/** A version tag: a frozen snapshot of an integration's definition. */
export interface Snapshot {
  id: string;
  integrationId: string;
  tag: string;
  /** RFC3339 timestamp of when the tag was created. */
  createdAt: string;
}

/** An authenticated principal, provisioned from the OIDC identity on first sign-in. */
export interface User {
  /** The durable orchestrator id; the stable handle per-user data is scoped by. */
  id: string;
  email: string;
  name: string;
  /** RFC3339 timestamp of when the user was first provisioned. */
  createdAt: string;
  /** RFC3339 timestamp of the most recent sign-in. */
  lastLoginAt: string;
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

/** Live state of one runtime pod backing a deployment. */
export interface PodStatus {
  name: string;
  /** Pending/Running/Succeeded/Failed/Unknown. */
  phase: string;
  ready: boolean;
  restarts: number;
}

/** One deployed instance of an integration running as its own workload. */
export interface Deployment {
  id: string;
  integrationId: string;
  /** Display name, captured from the integration at deploy time. */
  name: string;
  /** The version tag this deployment was created from; absent on legacy deployments. */
  tag?: string;
  /** Cached lifecycle status; refreshed by the orchestrator on read. */
  status: DeploymentStatus;
  /** Desired/served replica count (from settings). */
  replicas: number;
  /** Ready replica count, live from the cluster. */
  readyReplicas: number;
  /** Desired replica count, live from the cluster's Deployment spec. */
  desiredReplicas: number;
  /** Terminal failure reason (e.g. ImagePullBackOff), when failed. */
  reason?: string;
  /** Per-pod live detail. */
  pods?: PodStatus[];
  /** In-cluster address other flows use to reach this integration, if any. */
  internalUrl?: string;
  /** Public https URL when the deployment is exposed externally. */
  externalUrl?: string;
  /** RFC3339 timestamp of the workload's creation (age anchor), if known. */
  createdAt?: string;
  /** RFC3339 timestamp of the last status/state update. */
  lastUpdated: string;
}

/** How one declared env var is filled at deploy: a literal value or a secret ref. */
export interface EnvBindingInput {
  value?: string;
  secret?: string;
}

/** Per-deployment options sent when deploying an integration. */
export interface DeploymentInput {
  /** The version tag (snapshot id) to deploy; required by the orchestrator. */
  snapshotId?: string;
  /** Runtime replicas; omitted/<=0 means a single replica. */
  replicas?: number;
  /** User-chosen internal address slug; omitted asks the orchestrator to allocate. */
  slug?: string;
  /** "external" publishes a {slug}.{baseDomain} endpoint with TLS. */
  expose?: "external";
  /** External host label; defaults to the slug when omitted. */
  subdomain?: string;
  /** Bindings for the integration's declared env vars, keyed by var name. */
  env?: Record<string, EnvBindingInput>;
}

/** An environment variable an integration declares, for the modal to prompt on. */
export interface DeployEnvVar {
  name: string;
  default?: string;
  required?: boolean;
}

/**
 * Deploy choices for an integration, backing the deploy modal. When fetched with a
 * candidate slug the `slug*` fields validate it (for the requested exposure);
 * otherwise `suggestedSlug` carries a free default to prefill.
 */
export interface DeployOptions {
  /** Whether the integration has an HTTP source (so it gets a slug and can expose). */
  networked: boolean;
  /** A free slug to prefill the field with (only when no candidate was checked). */
  suggestedSlug?: string;
  /** The integration's declared env vars (excluding orchestrator-managed ones). */
  envVars?: DeployEnvVar[];
  /** Normalized form of the checked candidate. */
  slug?: string;
  /** The candidate has a usable form. */
  slugValid: boolean;
  /** The candidate is not already claimed (subdomain too, when external). */
  slugAvailable: boolean;
}

// --- Integrations ---------------------------------------------------------
// Backed by server actions in `app/actions/integrations.ts`; these wrappers unwrap
// the ActionResult so callers keep a value-or-throw contract.

export async function listIntegrations(): Promise<Integration[]> {
  return unwrap(await integrationActions.listIntegrations());
}

export async function getIntegration(id: string): Promise<Integration> {
  return unwrap(await integrationActions.getIntegration(id));
}

export async function createIntegration(
  input: IntegrationInput,
): Promise<Integration> {
  return unwrap(await integrationActions.createIntegration(input));
}

export async function updateIntegration(
  id: string,
  input: IntegrationInput,
): Promise<Integration> {
  return unwrap(await integrationActions.updateIntegration(id, input));
}

export async function deleteIntegration(id: string): Promise<void> {
  return unwrap(await integrationActions.deleteIntegration(id));
}

// --- Deployments ----------------------------------------------------------
// Backed by server actions in `app/actions/deployments.ts`. The live event stream
// stays an SSE route (DeploymentsSection subscribes via EventSource).

/** List the deployments of an integration (status refreshed server-side on read). */
export async function listDeployments(
  integrationId: string,
): Promise<Deployment[]> {
  return unwrap(await deploymentActions.listDeployments(integrationId));
}

/**
 * Fetch deploy options for an integration. With no `slug` it returns whether the
 * integration is networked plus a suggested free slug; with a `slug` it validates
 * that candidate for the given exposure (external also checks the subdomain).
 */
export async function getDeployOptions(
  integrationId: string,
  opts: { slug?: string; expose?: "external"; snapshotId?: string } = {},
): Promise<DeployOptions> {
  return unwrap(await deploymentActions.getDeployOptions(integrationId, opts));
}

/** Deploy an integration as a new workload, optionally exposed externally. */
export async function createDeployment(
  integrationId: string,
  input: DeploymentInput = {},
): Promise<Deployment> {
  return unwrap(await deploymentActions.createDeployment(integrationId, input));
}

/** Roll a live deployment over to a different version tag (rolling update). */
export async function rolloutDeployment(
  id: string,
  snapshotId: string,
): Promise<Deployment> {
  return unwrap(await deploymentActions.rolloutDeployment(id, snapshotId));
}

/** Scale an existing deployment to a new desired replica count. */
export async function scaleDeployment(
  id: string,
  replicas: number,
): Promise<Deployment> {
  return unwrap(await deploymentActions.scaleDeployment(id, replicas));
}

/** Undeploy a deployment, removing its workload. */
export async function deleteDeployment(id: string): Promise<void> {
  return unwrap(await deploymentActions.deleteDeployment(id));
}

// --- Folders --------------------------------------------------------------
// Backed by server actions in `app/actions/folders.ts`; these wrappers unwrap the
// ActionResult so callers keep a value-or-throw contract.

export async function listFolders(): Promise<Folder[]> {
  return unwrap(await folderActions.listFolders());
}

export async function createFolder(
  name: string,
  parentId: string | null = null,
): Promise<Folder> {
  return unwrap(await folderActions.createFolder(name, parentId));
}

export async function renameFolder(
  id: string,
  name: string,
  parentId: string | null,
): Promise<Folder> {
  return unwrap(await folderActions.renameFolder(id, name, parentId));
}

export async function deleteFolder(id: string): Promise<void> {
  return unwrap(await folderActions.deleteFolder(id));
}

/** Persist the order of the folders under a parent (null for the root level). */
export async function reorderFolders(
  parentId: string | null,
  folderIds: string[],
): Promise<void> {
  return unwrap(await folderActions.reorderFolders(parentId, folderIds));
}

export async function listFolderIntegrations(
  folderId: string,
): Promise<Integration[]> {
  return unwrap(await folderActions.listFolderIntegrations(folderId));
}

/** Add an integration to a folder (single-membership: replaces any prior folder). */
export async function assignIntegration(
  folderId: string,
  integrationId: string,
): Promise<void> {
  return unwrap(await folderActions.assignIntegration(folderId, integrationId));
}

/** Remove an integration from a folder. */
export async function unassignIntegration(
  folderId: string,
  integrationId: string,
): Promise<void> {
  return unwrap(
    await folderActions.unassignIntegration(folderId, integrationId),
  );
}

/** Persist the manual order of a folder's integrations (full list, in order). */
export async function reorderFolderIntegrations(
  folderId: string,
  integrationIds: string[],
): Promise<void> {
  return unwrap(
    await folderActions.reorderFolderIntegrations(folderId, integrationIds),
  );
}

// --- Snapshots (version tags) ---------------------------------------------
// Backed by server actions in `app/actions/snapshots.ts`.

/** List an integration's version tags, newest first. */
export async function listSnapshots(
  integrationId: string,
): Promise<Snapshot[]> {
  return unwrap(await snapshotActions.listSnapshots(integrationId));
}

/** Freeze the integration's current definition under a new tag. */
export async function createSnapshot(
  integrationId: string,
  tag: string,
): Promise<Snapshot> {
  return unwrap(await snapshotActions.createSnapshot(integrationId, tag));
}

/** Delete a version tag (refused by the orchestrator if currently deployed). */
export async function deleteSnapshot(id: string): Promise<void> {
  return unwrap(await snapshotActions.deleteSnapshot(id));
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
