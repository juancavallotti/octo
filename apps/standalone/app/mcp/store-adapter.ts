import type {
  IntegrationRecord,
  IntegrationStore,
  MetaStore,
  ResourceRecord,
  ResourceStore,
} from "@octo/mcp";
import { publish } from "@octo/events";
import * as store from "../api/fs/store";
import * as resources from "../api/fs/resourceStore";

/**
 * The standalone host's {@link IntegrationStore}: a thin shim over the local disk
 * store the editor's filesystem capability already uses. "Integrations" here are
 * the `*.yaml` flow files under the store root; the MCP layer treats their id,
 * name, and definition uniformly. `update` renames on disk when a new name's slug
 * differs (matching the editor's save), otherwise overwrites in place.
 */

/**
 * Announce a write on the in-process bus so an editor with this file open can
 * live-reload it (see @octo/events). Returns the record for call-site convenience.
 */
function announce(rec: IntegrationRecord): IntegrationRecord {
  publish({ type: "integration.updated", id: rec.id, name: rec.name });
  return rec;
}

export const fsIntegrationStore: IntegrationStore = {
  list: () => store.listFlows(),
  get: (id) => store.readFlow(id),
  create: async (name, definition) =>
    announce(await store.createFlow(name, definition)),
  update: async (id, name, definition) =>
    announce(
      name === undefined
        ? await store.writeFlow(id, definition)
        : await store.updateFlow(id, name, definition),
    ),
};

/** Env-convention names are env resources; everything else is a template. */
function kindFor(name: string): "env" | "template" {
  const base = (name.toLowerCase().split("/").pop() ?? "").trim();
  return base === ".env" || base.startsWith(".env.") || base.endsWith(".env")
    ? "env"
    : "template";
}

/** Build a {@link ResourceRecord} from a disk file: its name doubles as its id. */
function toRecord(
  integrationId: string,
  name: string,
  content: string,
): ResourceRecord {
  return { id: name, integrationId, kind: kindFor(name), name, content };
}

/**
 * The standalone host's {@link ResourceStore}: a thin shim over the flat local-disk
 * resource store the editor's Resources tab already uses. Storage is shared across
 * flows (no per-integration partition on disk), so the integration id is echoed but
 * not used to locate files, and a resource's path-like name doubles as its id (kind
 * is inferred from the name). `update` renames when the name changes, then rewrites
 * content — matching the editor's move-then-save.
 */
export const fsResourceStore: ResourceStore = {
  list: async (integrationId) =>
    (await resources.listResources()).map((r) =>
      toRecord(integrationId, r.name, r.content),
    ),
  get: async (integrationId, resourceId) => {
    const content = await resources.readResource(resourceId);
    if (content === null) throw new Error(`no such resource: ${resourceId}`);
    return toRecord(integrationId, resourceId, content);
  },
  create: async (integrationId, _kind, name, content) => {
    if (!name.trim()) throw new Error("name is required");
    if ((await resources.readResource(name)) !== null) {
      throw new Error("a resource with that name already exists");
    }
    await resources.writeResource(name, content);
    return toRecord(integrationId, name, content);
  },
  update: async (integrationId, resourceId, _kind, name, content) => {
    if (!name.trim()) throw new Error("name is required");
    if (name !== resourceId) {
      if ((await resources.readResource(name)) !== null) {
        throw new Error("a resource with that name already exists");
      }
      await resources.renameResource(resourceId, name);
    }
    await resources.writeResource(name, content);
    return toRecord(integrationId, name, content);
  },
  remove: async (_integrationId, resourceId) => {
    await resources.deleteResource(resourceId);
  },
};

/**
 * The editor-meta file name, mirroring the editor's own EDITOR_META_RESOURCE and the
 * `editorMeta` server action — the store an agent writes through has to be the same file
 * the canvas reads, or the mocks it places will not be there when the user looks.
 */
const EDITOR_META_RESOURCE = ".octo/editor-meta.json";

/**
 * The standalone host's {@link MetaStore}: `.octo/editor-meta.json` under the flows
 * directory, beside the flows it describes.
 *
 * Storage here is flat and shared across every flow file, so the integration id names no
 * file — one document describes the whole directory. It is still keyed by that id
 * *inside* the file, which is why the id is passed through untouched rather than dropped.
 */
export const fsMetaStore: MetaStore = {
  load: async () => (await resources.readResource(EDITOR_META_RESOURCE)) ?? "",
  save: async (_integrationId, content) => {
    await resources.writeResource(EDITOR_META_RESOURCE, content);
  },
};
