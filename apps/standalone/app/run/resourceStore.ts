import type { ResourceStore } from "@octo/editor";
import {
  createResourceAction,
  deleteResourceAction,
  listResourcesAction,
  moveResourceAction,
  updateResourceAction,
} from "../actions/resources";
import { unwrap } from "../actions/result";

/**
 * The standalone resource store: the editor's Resources tab reads and writes
 * resource files on local disk (OCTO_FS_DIR) through server actions. Storage is
 * flat and shared across flows, so a resource's path-like name is its id — there
 * is no per-integration partition. Files that exist on disk but the open flow
 * doesn't declare surface as "not in project" in the tab and are never mutated
 * unless the user includes them.
 */
export const localDiskResourceStore: ResourceStore = {
  async list() {
    return unwrap(await listResourcesAction());
  },
  async create({ name, content }) {
    return unwrap(await createResourceAction(name, content));
  },
  async update(id, patch) {
    return unwrap(await updateResourceAction(id, patch.content ?? ""));
  },
  async move(id, name) {
    return unwrap(await moveResourceAction(id, name));
  },
  async remove(id) {
    unwrap(await deleteResourceAction(id));
  },
};
