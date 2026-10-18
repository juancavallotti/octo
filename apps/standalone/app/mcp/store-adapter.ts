import type {
  IntegrationRecord,
  IntegrationStore,
  MetaStore,
  ResourceRecord,
  ResourceStore,
  SuiteStore,
} from "@octo/mcp";
import { publish } from "@octo/events";
import * as store from "../api/fs/store";
import * as resources from "../api/fs/resourceStore";
import * as suites from "../api/fs/testSuiteStore";

/**
 * This host's {@link IntegrationStore}: a shim over the local disk store. This app serves
 * one integration — the folder, whose config files are its `*.yaml` files — so there is
 * one record, and creating a second is refused rather than allowed to overwrite the
 * first. A name is not stored apart from the files: it is the `service.name` they
 * declare, so renaming is an edit to that file.
 */

/**
 * Announce a write on the in-process bus so an editor with this project open can
 * live-reload it (see @octo/events). Returns the record for call-site convenience.
 */
function announce(rec: IntegrationRecord): IntegrationRecord {
  publish({ type: "integration.updated", id: rec.id, name: rec.name });
  return rec;
}

async function project(id: string): Promise<IntegrationRecord> {
  const rec = await store.readProject();
  if (id !== rec.id) throw new Error(`no such integration: ${id} (this app serves "${rec.id}")`);
  return rec;
}

export const fsIntegrationStore: IntegrationStore = {
  list: async () => {
    const { id, name } = await store.readProject();
    return [{ id, name }];
  },
  get: project,
  create: async (_name, files) => {
    const current = await store.readProject();
    if (current.files.length > 0) {
      throw new Error(
        `this app serves one integration, "${current.id}", and it already has config files — change it with write_file, update_flow or update_integration`,
      );
    }
    return announce(await store.writeProject(files));
  },
  update: async (id, _name, files) => {
    await project(id);
    return announce(await store.writeProject(files));
  },
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
 * This host's {@link ResourceStore}: a shim over the flat local-disk resource store.
 * Storage is shared across flows, so the integration id is echoed but never used to
 * locate a file, and a resource's path-like name doubles as its id. `update` renames
 * when the name changes, then rewrites content.
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
 * The editor-meta file name, mirroring the editor's own EDITOR_META_RESOURCE: a write
 * through this store must land in the file the canvas reads.
 */
const EDITOR_META_RESOURCE = ".octo/editor-meta.json";

/**
 * This host's {@link MetaStore}: `.octo/editor-meta.json` under the flows directory,
 * beside the flows it describes. Storage is flat, so the integration id names no file —
 * one document describes the whole directory — but the id still keys entries inside it,
 * so it is passed through untouched.
 */
export const fsMetaStore: MetaStore = {
  load: async () => (await resources.readResource(EDITOR_META_RESOURCE)) ?? "",
  save: async (integrationId, content) => {
    await resources.writeResource(EDITOR_META_RESOURCE, content);
    // A subscriber showing this document's mocks and spies would otherwise keep the
    // ones it loaded until a reload.
    publish({ type: "integration.meta-updated", id: integrationId });
  },
};

/**
 * This host's {@link SuiteStore}: the `*_test.yaml` files sitting beside the flows, the
 * same files `dolphin test` runs from a terminal. Flat and shared across documents like
 * the rest of the local store, so a flow name identifies its suite within the root.
 */
export const fsSuiteStore: SuiteStore = {
  list: () => suites.listSuites(),
  save: async (integrationId, flow, content) => {
    await suites.writeSuite(flow, content);
    publish({ type: "integration.tests-updated", id: integrationId });
  },
  remove: async (integrationId, flow) => {
    await suites.deleteSuite(flow);
    publish({ type: "integration.tests-updated", id: integrationId });
  },
};
