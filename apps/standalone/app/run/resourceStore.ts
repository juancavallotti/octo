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
 * The local-disk resource store, backed by server actions over OCTO_FS_DIR. Storage is
 * flat and shared across flows, so a resource's path-like name is its id. Files on disk
 * that the open flow does not declare are reported as such and never mutated unless
 * they are included.
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
