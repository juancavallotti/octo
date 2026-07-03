import type { ResourceStore, StoredResource } from "@octo/editor";
import {
  listResources,
  createResource,
  updateResource,
  deleteResource,
} from "@/app/actions/resources";
import { unwrap } from "@/app/model/bff";
import type { Resource } from "@/app/model/orchestrator";

/**
 * The platform resource store: backs the editor's Resources tab with the
 * integration's resources in the orchestrator, through the auth-gated resource
 * actions. A resource needs an owning integration, so the id is read lazily via a
 * getter — the same one TagButton uses — so it survives the mint-on-first-save
 * flow (an unsaved draft has no id). Until an id exists, `list()` is empty and
 * mutations throw a readable error rather than hitting the orchestrator.
 */

function toStored(r: Resource): StoredResource {
  return {
    id: r.id,
    name: r.name,
    kind: r.kind === "env" ? "env" : "template",
    content: r.content,
  };
}

export function makeResourceStore(
  getIntegrationId: () => string | null,
): ResourceStore {
  const requireId = () => {
    const id = getIntegrationId();
    if (!id) throw new Error("Save the integration before managing resources.");
    return id;
  };

  return {
    async list() {
      const id = getIntegrationId();
      if (!id) return [];
      return unwrap(await listResources(id)).map(toStored);
    },

    async create({ name, kind, content }) {
      const id = requireId();
      return toStored(unwrap(await createResource(id, kind, name, content)));
    },

    async update(resourceId, patch) {
      const id = requireId();
      // updateResource replaces the whole record, so merge the patch over the
      // current row (mirrors upsertResourceByName's list-then-write).
      const current = unwrap(await listResources(id)).find(
        (r) => r.id === resourceId,
      );
      if (!current) throw new Error("Resource no longer exists.");
      const kind = patch.kind ?? (current.kind === "env" ? "env" : "template");
      const content = patch.content ?? current.content;
      return toStored(
        unwrap(await updateResource(id, resourceId, kind, current.name, content)),
      );
    },

    async move(resourceId, name) {
      const id = requireId();
      // A resource keeps its uuid across a rename; only its name changes.
      const current = unwrap(await listResources(id)).find(
        (r) => r.id === resourceId,
      );
      if (!current) throw new Error("Resource no longer exists.");
      const kind = current.kind === "env" ? "env" : "template";
      return toStored(
        unwrap(await updateResource(id, resourceId, kind, name, current.content)),
      );
    },

    async remove(resourceId) {
      const id = requireId();
      unwrap(await deleteResource(id, resourceId));
    },
  };
}
