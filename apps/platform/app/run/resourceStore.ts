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
 * actions. A resource needs an owning integration, so the store owns the id
 * mutably and exposes `setIntegrationId` — the first save mints one and pushes it
 * in — so the store survives the mint-on-first-save flow without being recreated
 * (an unsaved draft has no id). Until an id exists, `list()` is empty and
 * mutations throw a readable error rather than hitting the orchestrator.
 */
export interface PlatformResourceStore extends ResourceStore {
  /** Point the store at the integration whose resources it manages. */
  setIntegrationId(id: string | null): void;
}

function toStored(r: Resource): StoredResource {
  return {
    id: r.id,
    name: r.name,
    kind: r.kind === "env" ? "env" : "template",
    content: r.content,
  };
}

export function makeResourceStore(
  initialIntegrationId: string | null,
): PlatformResourceStore {
  let integrationId = initialIntegrationId;
  const requireId = () => {
    if (!integrationId)
      throw new Error("Save the integration before managing resources.");
    return integrationId;
  };

  return {
    setIntegrationId(id) {
      integrationId = id;
    },

    async list() {
      const id = integrationId;
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
