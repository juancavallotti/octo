/**
 * The platform's FileSystemCapability: backs the editor's load/save by the
 * orchestrator, through the browser client in `app/model/orchestrator.ts`. A project
 * is an integration; its files are the integration's config files, written as one set
 * so a change spanning files never passes through a state that would not merge.
 */

import {
  assignIntegration,
  createIntegrationWithFiles,
  findIntegrationFolderId,
  getConfigFiles,
  getIntegration,
  listFolders,
  listIntegrations,
  replaceConfigFiles,
  unassignIntegration,
  updateIntegration,
} from "@/app/model/orchestrator";
import type { FileSystemCapability } from "@octo/editor";

export const orchestratorFileSystem: FileSystemCapability = {
  async load(id) {
    // The integration record doesn't carry its folder, so resolve it from
    // membership (single-membership tree). Matches the old IntegrationLoader.
    const [integration, files, folderId] = await Promise.all([
      getIntegration(id),
      getConfigFiles(id),
      findIntegrationFolderId(id),
    ]);
    return {
      id: integration.id,
      name: integration.name,
      files: files.map(({ path, content }) => ({ path, content })),
      folderId,
      lastUpdated: integration.lastUpdated,
    };
  },

  async save(id, { name, files, folderId }) {
    if (id) {
      // Updates don't move folders here (the folder picker applies that live);
      // pass the known folderId back through for the saved snapshot. The files go
      // first: a set the orchestrator refuses leaves the name as it was too.
      await replaceConfigFiles(id, files);
      const updated = await updateIntegration(id, { name });
      return { ...updated, files, folderId: folderId ?? null };
    }
    const created = await createIntegrationWithFiles(name, files);
    if (folderId) await assignIntegration(folderId, created.id);
    return { ...created, files, folderId: folderId ?? null };
  },

  async list() {
    return listIntegrations();
  },

  folders: {
    list: () => listFolders(),
    assign: (folderId, projectId) => assignIntegration(folderId, projectId),
    unassign: (folderId, projectId) => unassignIntegration(folderId, projectId),
  },
};
