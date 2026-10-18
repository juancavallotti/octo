/**
 * The standalone FileSystemCapability: the folder is the project, loaded and saved
 * through server actions that read and write its `*.yaml` config files. One project per
 * folder, so the id only names it, and no folder capability — the editor's folder UI
 * stays hidden.
 */

import type { FileSystemCapability } from "@octo/editor";
import { listProjects, loadProject, saveProject } from "../actions/fs";
import { unwrap } from "../actions/result";

export const localDiskFileSystem: FileSystemCapability = {
  async load() {
    return unwrap(await loadProject());
  },

  async save(id, input) {
    // The project's name lives in its `service.name`, which the editor has already
    // written into the files. The id is checked server-side against the folder.
    return unwrap(await saveProject(id, input.files));
  },

  async list() {
    return unwrap(await listProjects());
  },
};
