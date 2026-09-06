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

// The type surface lives next door and is re-exported, so every existing
// `@/app/model/orchestrator` import keeps working and callers need not care
// which half a name comes from.
export * from "./orchestratorTypes";
// Bundles (and the frozen-resource read) live next door: they are the calls whose
// payload is bytes rather than JSON, and they carry the base64 hop that goes with it.
export * from "./bundles";
// Resource CRUD is its own module for the same reason the file split exists at
// all: one nameable concern per file.
export * from "./resources";
import type {
  DeployOptions,
  Deployment,
  DeploymentInput,
  EnvBindingInput,
  Folder,
  Integration,
  IntegrationInput,
  Snapshot,
  SnapshotResource,
} from "./orchestratorTypes";

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

/** One deployment by id, for a caller that has the id and nothing else. */
export async function getDeployment(id: string): Promise<Deployment> {
  return unwrap(await deploymentActions.getDeployment(id));
}

/** A deployment paired with the display name of the integration it belongs to. */
export type DeploymentWithIntegration = Deployment & {
  integrationName: string;
};

/**
 * Aggregate every deployment across every integration into one flat, named list.
 * A per-integration failure contributes nothing rather than failing the whole
 * call, so one unreachable integration can't blank the view. Shared by the
 * dashboard, the deployments page, and the object browser's deployment picker.
 */
export async function listAllDeployments(): Promise<
  DeploymentWithIntegration[]
> {
  const integrations = await listIntegrations();
  const lists = await Promise.all(
    integrations.map((i) =>
      listDeployments(i.id).then(
        (ds) => ds.map((d) => ({ ...d, integrationName: i.name })),
        () => [] as DeploymentWithIntegration[],
      ),
    ),
  );
  return lists.flat();
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

/**
 * Roll a live deployment over to a different version tag (rolling update).
 * An `env` map replaces the deployment's stored bindings (edit/extend on rollout);
 * omitting it preserves them (a plain version bump keeps the same env). `tracing`
 * follows the same rule — omitted keeps the deployment's setting, so a version bump
 * never silently stops tracing a deployment someone is investigating.
 */
export async function rolloutDeployment(
  id: string,
  snapshotId: string,
  env?: Record<string, EnvBindingInput>,
  tracing?: boolean,
): Promise<Deployment> {
  return unwrap(
    await deploymentActions.rolloutDeployment(id, snapshotId, env, tracing),
  );
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

/** List the resources frozen alongside a tag's definition (metadata only). */
export async function listSnapshotResources(
  snapshotId: string,
): Promise<SnapshotResource[]> {
  return unwrap(await snapshotActions.listSnapshotResources(snapshotId));
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
