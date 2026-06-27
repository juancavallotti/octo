import type { IntegrationRecord, IntegrationStore } from "@octo/mcp";
import type { ActionResult } from "@octo/http";
import { publish } from "@octo/events";
import type { Integration } from "@/app/model/orchestrator";
import * as client from "@/app/actions/_client";

/**
 * The platform host's {@link IntegrationStore}: a thin shim over the orchestrator
 * integration API, reached through the same typed client the server actions use.
 * The MCP route authenticates the caller itself (bearer API key), so it talks to
 * the client directly rather than through the OIDC-gated actions. Each call
 * unwraps the ActionResult — a thrown error becomes a clean MCP tool error via the
 * package's guard.
 */

/** Unwrap an orchestrator result, throwing its error so the MCP tool layer reports it. */
function unwrap<T>(res: ActionResult<T>): T {
  if (!res.ok) throw new Error(res.error);
  return res.data;
}

/** Drop the orchestrator's bookkeeping, keeping the fields the MCP layer uses. */
function toRecord(it: Integration): IntegrationRecord {
  return { id: it.id, name: it.name, definition: it.definition };
}

/**
 * Announce a write on the in-process bus so an editor with this file open can
 * live-reload it (see @octo/events). Fire-and-forget: never let it affect the
 * write's result. Returns the record for call-site convenience.
 */
function announce(rec: IntegrationRecord): IntegrationRecord {
  publish({ type: "integration.updated", id: rec.id, name: rec.name });
  return rec;
}

export const orchestratorIntegrationStore: IntegrationStore = {
  list: async () => {
    const items = unwrap(await client.listIntegrations());
    return items.map((it) => ({ id: it.id, name: it.name }));
  },
  get: async (id) => toRecord(unwrap(await client.getIntegration(id))),
  create: async (name, definition) =>
    announce(toRecord(unwrap(await client.createIntegration({ name, definition })))),
  update: async (id, name, definition) => {
    // The orchestrator's update requires a name; when the caller isn't renaming,
    // preserve the integration's current name rather than blanking it.
    const resolved = name ?? unwrap(await client.getIntegration(id)).name;
    return announce(
      toRecord(
        unwrap(
          await client.updateIntegration(id, { name: resolved, definition }),
        ),
      ),
    );
  },
};
