import { integrationEventStream } from "@octo/events";

/**
 * GET /api/integrations/events — Server-Sent Events stream of integration-write
 * events from the in-process bus (@octo/events). The MCP store adapter publishes
 * when it creates/updates an integration; the editor subscribes here and
 * live-reloads the file it has open. Gated by the OIDC session like other
 * /api/* routes (the proxy 401s unauthenticated callers), so only signed-in
 * editors receive the stream.
 *
 * In-process bus: in a multi-replica deploy a write handled by one replica is
 * only seen by editors connected to that same replica (same limitation as the
 * RUN feature). Acceptable for a best-effort reload hint.
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: Request) {
  return integrationEventStream(req.signal);
}
