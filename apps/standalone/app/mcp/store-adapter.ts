import type { IntegrationRecord, IntegrationStore } from "@octo/mcp";
import { publish } from "@octo/events";
import * as store from "../api/fs/store";

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
