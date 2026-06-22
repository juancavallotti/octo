import { snapshot, subscribe } from "../session";
import { type LogLine } from "../logbuffer";
import { ensureNamespace } from "../namespace";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** How often to send an SSE comment so proxies keep the connection open. */
const KEEPALIVE_MS = 15000;

/**
 * GET /api/run/logs — Server-Sent Events stream of this user's runner log lines.
 * On connect it replays the namespace's buffered lines, then streams new ones live
 * until the client disconnects (which cancels the stream and unsubscribes).
 */
export function GET(req: Request) {
  const { ns, setCookie } = ensureNamespace(req);
  const encoder = new TextEncoder();
  let cleanup = () => {};

  const stream = new ReadableStream({
    start(controller) {
      const send = (line: LogLine) => {
        controller.enqueue(
          encoder.encode(`id: ${line.seq}\ndata: ${line.text}\n\n`),
        );
      };
      for (const line of snapshot(ns)) send(line);
      const unsubscribe = subscribe(ns, send);
      const ping = setInterval(() => {
        controller.enqueue(encoder.encode(`: keep-alive\n\n`));
      }, KEEPALIVE_MS);
      cleanup = () => {
        clearInterval(ping);
        unsubscribe();
      };
    },
    cancel() {
      cleanup();
    },
  });

  const headers: Record<string, string> = {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache, no-transform",
    Connection: "keep-alive",
    "X-Accel-Buffering": "no",
  };
  if (setCookie) headers["Set-Cookie"] = setCookie;
  return new Response(stream, { headers });
}
