import { integrationEventStream } from "@octo/events";

/**
 * GET /api/integrations/events — Server-Sent Events stream of integration-write
 * events from the in-process bus (@octo/events). The MCP store adapter publishes
 * when it creates/updates a flow file; the editor subscribes here and live-reloads
 * the file it has open. The standalone app is single-process, so the in-process
 * bus reaches every editor.
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: Request) {
  return integrationEventStream(req.signal);
}
