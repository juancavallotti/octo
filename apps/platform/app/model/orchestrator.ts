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

// Re-exported so `@/app/model/orchestrator` is the single import for the type
// surface, the byte-payload bundle calls and resource CRUD alike.
export * from "./orchestratorTypes";
export * from "./bundles";
export * from "./resources";
export * from "./configFiles";
import type {
  DeployOptions,
  Deployment,
  DeploymentInput,
  RolloutChange,
  Runtimes,
  Folder,
  Integration,
  IntegrationDetailData,
  IntegrationInput,
  IntegrationProject,
  IntegrationRename,
  IntegrationTree,
  Snapshot,
  SnapshotDetail,
  SnapshotResource,
} from "./orchestratorTypes";

// --- Integrations ---------------------------------------------------------
// Backed by server actions in `app/actions/integrations.ts`; these wrappers unwrap
// the ActionResult so callers keep a value-or-throw contract.

export async function listIntegrations(): Promise<Integration[]> {
  return unwrap(await integrationActions.listIntegrations());
}

/** An integration, its config files and its folder, for the editor to open. */
export async function loadIntegrationProject(
  id: string,
): Promise<IntegrationProject> {
  return unwrap(await integrationActions.loadIntegrationProject(id));
}

/** The first view of an integration's detail pane, in one round trip. */
export async function loadIntegrationDetail(
  id: string,
): Promise<IntegrationDetailData> {
  return unwrap(await integrationActions.loadIntegrationDetail(id));
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
  input: IntegrationRename,
): Promise<Integration> {
  return unwrap(await integrationActions.updateIntegration(id, input));
}

export async function setIntegrationIcon(
  id: string,
  icon: string,
): Promise<Integration> {
  return unwrap(await integrationActions.setIntegrationIcon(id, icon));
}

export async function deleteIntegration(id: string): Promise<void> {
  return unwrap(await integrationActions.deleteIntegration(id));
}

// --- Deployments ----------------------------------------------------------
// Backed by server actions in `app/actions/deployments.ts`. The live event stream
// stays an SSE route.

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

/** Every deployment across every integration, newest first, each named. */
export async function listAllDeployments(): Promise<
  DeploymentWithIntegration[]
> {
  return unwrap(await deploymentActions.listAllDeployments());
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
 * and `runtimeVersion` follow the same rule — omitted keeps the deployment's
 * setting, so a version bump never silently stops tracing a deployment someone is
 * investigating, or moves one off the runtime it is pinned to.
 */
export async function rolloutDeployment(
  id: string,
  change: RolloutChange,
): Promise<Deployment> {
  return unwrap(await deploymentActions.rolloutDeployment(id, change));
}

/** The runtime releases a deployment can be pinned to. */
export async function listRuntimes(): Promise<Runtimes> {
  return unwrap(await deploymentActions.listRuntimes());
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

/** Folders, integrations, membership and what is deployed, in one round trip. */
export async function loadIntegrationTree(): Promise<IntegrationTree> {
  return unwrap(await folderActions.loadIntegrationTree());
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

/** One version tag with its frozen definition. */
export async function getSnapshot(id: string): Promise<SnapshotDetail> {
  return unwrap(await snapshotActions.getSnapshot(id));
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

