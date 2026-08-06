import { deriveNamespace, ensureNamespace, type RunKey } from "@octo/run-host";
import { currentUserId } from "@/app/actions/_auth";
import { remoteRunner } from "@/app/run/remoteRunner";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** How often to send an SSE comment so proxies keep the connection open. */
const KEEPALIVE_MS = 15000;

/**
 * GET /api/run/logs — Server-Sent Events stream of the dev run's log lines.
 *
 * A route rather than a server action because actions cannot stream, and it is the one
 * piece of the RUN feature that still needs a route at all. The lines come from the
 * orchestrator, which reads them from the run's pod — so this holds no buffer, and any
 * replica can serve the stream for a run any other replica started.
 *
 * The run is addressed by the caller's own user id (from the session) and the integration
 * named in the query string. Both halves matter: the integration is untrusted client
 * input, and pairing it with a server-resolved user is what keeps one user's stream from
 * reaching another's run — naming somebody else's integration simply finds nothing.
 */
export async function GET(req: Request) {
  const url = new URL(req.url);

  // The namespace is not what a dev run is keyed on — it is carried because the run key
  // has a slot for it, and resolving it here keeps two things the route would otherwise
  // lose: the run cookie stays fresh (an SSE connect is often a session's first request),
  // and `deriveNamespace` validates the untrusted tab id rather than passing it along.
  const { ns: base, setCookie } = ensureNamespace(req);

  let userId: string;
  try {
    userId = await currentUserId();
  } catch (err) {
    return new Response((err as Error).message, { status: 401 });
  }

  const key: RunKey = {
    namespace: deriveNamespace(base, url.searchParams.get("tab")),
    userId,
    integrationId: url.searchParams.get("integrationId") || undefined,
  };

  // Whether there is a run to follow is settled before the stream opens, deliberately. An
  // EventSource retries a stream that merely closes — forever, every few seconds — but
  // treats a non-200 as final, so the honest answer to "nothing is running" is a status
  // code and not an empty stream.
  let running: boolean;
  try {
    running = (await remoteRunner.status(key)).running;
  } catch (err) {
    return new Response((err as Error).message, { status: 502 });
  }
  if (!running) {
    return new Response("no dev run for this integration", { status: 404 });
  }

  // Resume after the last line the client showed, so its own de-duplication (which drops
  // anything at or below that sequence) does not discard fresh lines. See the runner's
  // followLogs for why a resumed stream still repeats the replayed tail.
  const lastId = req.headers.get("Last-Event-ID");
  const fromSeq = lastId !== null && /^\d+$/.test(lastId) ? Number(lastId) : undefined;

  const abort = new AbortController();
  const encoder = new TextEncoder();

  const stream = new ReadableStream({
    async start(controller) {
      /** Enqueue unless the consumer has gone; enqueueing on a closed stream throws. */
      const send = (text: string): boolean => {
        try {
          controller.enqueue(encoder.encode(text));
          return true;
        } catch {
          return false;
        }
      };

      // Open with a comment so the browser sees the connection established even when the
      // run has nothing to say yet — a pod pulling its image can be quiet for a while.
      send(": connected\n\n");
      const ping = setInterval(() => send(": keep-alive\n\n"), KEEPALIVE_MS);

      try {
        for await (const line of remoteRunner.logs(key, {
          fromSeq,
          signal: abort.signal,
        })) {
          if (!send(`id: ${line.seq}\ndata: ${line.text}\n\n`)) break;
        }
      } catch {
        // The stream ended badly — the pod went away, the orchestrator restarted. There is
        // nowhere to report it on an SSE channel, and the client will reconnect; a run that
        // is genuinely gone then gets the 404 above instead of a silent retry loop.
      } finally {
        clearInterval(ping);
        try {
          controller.close();
        } catch {
          // Already closed by the consumer cancelling.
        }
      }
    },
    cancel() {
      abort.abort();
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
