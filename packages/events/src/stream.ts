import { subscribe } from "./bus";
import type { OctoEvent } from "./types";

/** How often to send an SSE comment so proxies keep the connection open. */
const KEEPALIVE_MS = 15000;

/**
 * Build a Server-Sent Events {@link Response} that streams every bus event to the
 * client as a JSON `data:` frame until the request is aborted. Apps mount it from
 * a thin route handler (e.g. GET /api/integrations/events); the editor subscribes
 * with an EventSource and reloads when an event names the file it has open.
 *
 * Pass the request's `AbortSignal` so a disconnecting client unsubscribes; the
 * stream's own `cancel` covers the same teardown for good measure.
 */
export function integrationEventStream(signal?: AbortSignal): Response {
  const encoder = new TextEncoder();
  let cleanup = () => {};

  const stream = new ReadableStream({
    start(controller) {
      const send = (event: OctoEvent) => {
        controller.enqueue(
          encoder.encode(`data: ${JSON.stringify(event)}\n\n`),
        );
      };
      const unsubscribe = subscribe(send);
      const ping = setInterval(() => {
        controller.enqueue(encoder.encode(`: keep-alive\n\n`));
      }, KEEPALIVE_MS);
      cleanup = () => {
        clearInterval(ping);
        unsubscribe();
      };
      signal?.addEventListener("abort", () => {
        cleanup();
        try {
          controller.close();
        } catch {
          // Already closed — nothing to do.
        }
      });
    },
    cancel() {
      cleanup();
    },
  });

  return new Response(stream, {
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
