/**
 * Server-side helper for streaming Server-Sent Events from the orchestrator. The
 * request/response BFF calls have all moved to server actions (the high-level
 * orchestrator client in `app/actions/_client.ts`); only SSE remains a route,
 * because server actions can't back an `EventSource`. This keeps the orchestrator
 * URL a server-only secret for the surviving stream route.
 *
 * The orchestrator's base URL comes from the `ORCHESTRATOR_URL` env var; when it
 * is unset the stream answers 503.
 */

/** The orchestrator base URL with any trailing slash trimmed, or "" when unset. */
function baseUrl(): string {
  return (process.env.ORCHESTRATOR_URL ?? "").replace(/\/+$/, "");
}

function notConfigured(): Response {
  return Response.json(
    { error: "orchestrator not configured (ORCHESTRATOR_URL unset)" },
    { status: 503 },
  );
}

/**
 * Stream a Server-Sent Events response from `path` on the orchestrator straight
 * through to the caller. The caller's AbortSignal is forwarded, so when the
 * browser closes the EventSource the upstream connection is torn down too. Returns
 * 503 when no orchestrator is configured and 502 when it is unreachable; a
 * non-streamable upstream response is relayed with its status so the client's
 * EventSource sees the error.
 */
export async function stream(
  path: string,
  signal?: AbortSignal,
): Promise<Response> {
  const base = baseUrl();
  if (!base) return notConfigured();

  let res: Response;
  try {
    res = await fetch(`${base}${path}`, {
      signal,
      headers: { Accept: "text/event-stream" },
    });
  } catch (err) {
    return Response.json(
      { error: `orchestrator unreachable: ${(err as Error).message}` },
      { status: 502 },
    );
  }

  if (!res.ok || !res.body) {
    return new Response(null, { status: res.status });
  }
  return new Response(res.body, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
