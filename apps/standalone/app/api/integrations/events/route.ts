import { integrationEventStream } from "@octo/events";
import { startWatching } from "../../fs/watch";

/**
 * GET /api/integrations/events — Server-Sent Events stream of integration-write
 * events from the in-process bus (@octo/events). The MCP store adapter publishes
 * when it creates/updates a flow file, and the filesystem watcher publishes when
 * anything else does; the editor subscribes here and live-reloads the file it has
 * open. The standalone app is single-process, so the in-process bus reaches every
 * editor.
 *
 * The watcher is started from here rather than at module load, so it exists exactly
 * while somebody is listening: a request for this stream IS an editor being open.
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: Request) {
  startWatching();
  return integrationEventStream(req.signal);
}
