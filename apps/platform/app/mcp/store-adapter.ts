import type {
  IntegrationRecord,
  IntegrationStore,
  ResourceRecord,
  ResourceStore,
} from "@octo/mcp";
import type { ActionResult } from "@octo/http";
import type { Integration, Resource } from "@/app/model/orchestrator";
import { publishIntegrationEvent } from "@/app/lib/integrationEvents";
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
 * Announce a write so an editor with this file open can live-reload it (NATS when
 * configured, else the in-process bus — see {@link publishIntegrationEvent}).
 * Fire-and-forget: never let it affect the write's result. Returns the record for
 * call-site convenience.
 */
function announce(rec: IntegrationRecord): IntegrationRecord {
  void publishIntegrationEvent({
    type: "integration.updated",
    id: rec.id,
    name: rec.name,
  });
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

/** Drop the orchestrator's timestamps, coercing kind to the MCP union. */
function toResource(r: Resource): ResourceRecord {
  return {
    id: r.id,
    integrationId: r.integrationId,
    kind: r.kind === "env" ? "env" : "template",
    name: r.name,
    content: r.content,
  };
}

/**
 * The platform host's {@link ResourceStore}: a thin shim over the orchestrator's
 * nested `/integrations/{id}/resources` routes, through the same typed client the
 * server actions use. Every resource is addressed within its owning integration —
 * a resource id alone is not a valid address. `update` is a full replace (the
 * MCP tool layer fills any omitted kind/name from `get` first).
 */
export const orchestratorResourceStore: ResourceStore = {
  list: async (integrationId) =>
    unwrap(await client.listResources(integrationId)).map(toResource),
  get: async (integrationId, resourceId) =>
    toResource(unwrap(await client.getResource(integrationId, resourceId))),
  create: async (integrationId, kind, name, content) =>
    toResource(
      unwrap(await client.createResource(integrationId, kind, name, content)),
    ),
  update: async (integrationId, resourceId, kind, name, content) =>
    toResource(
      unwrap(
        await client.updateResource(integrationId, resourceId, kind, name, content),
      ),
    ),
  remove: async (integrationId, resourceId) => {
    unwrap(await client.deleteResource(integrationId, resourceId));
  },
};
