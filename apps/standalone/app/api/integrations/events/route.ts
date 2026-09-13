import { integrationEventStream } from "@octo/events";
import { startWatching } from "../../fs/watch";

/**
 * GET /api/integrations/events — Server-Sent Events stream of integration-write
 * events from the in-process bus. A subscriber live-reloads the file it has open; this
 * app is one process, so the in-process bus reaches all of them.
 *
 * The filesystem watcher is started from here rather than at module load, so it runs
 * exactly while somebody is listening.
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: Request) {
  startWatching();
  return integrationEventStream(req.signal);
}
