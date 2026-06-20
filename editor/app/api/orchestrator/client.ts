/**
 * Server-side BFF helper for talking to the orchestrator. The editor never calls
 * the orchestrator from the browser — these proxy routes front every call so the
 * orchestrator needs no CORS, and so its URL stays a server-only secret. Mirrors
 * the server-only module style of `app/api/run/session.ts`.
 *
 * The orchestrator's base URL comes from the `ORCHESTRATOR_URL` env var. When it
 * is unset the integration features are simply disabled: `forward`/`proxy` answer
 * 503 and the availability probe reports `available: false`.
 */

/** The orchestrator base URL with any trailing slash trimmed, or "" when unset. */
function baseUrl(): string {
  return (process.env.ORCHESTRATOR_URL ?? "").replace(/\/+$/, "");
}

/** Whether an orchestrator URL is configured. */
export function orchestratorConfigured(): boolean {
  return baseUrl() !== "";
}

function notConfigured(): Response {
  return Response.json(
    { error: "orchestrator not configured (ORCHESTRATOR_URL unset)" },
    { status: 503 },
  );
}

/**
 * Forward a request to `path` on the orchestrator and relay its response verbatim
 * (status code + JSON body). Returns 503 when no orchestrator is configured and
 * 502 when it is unreachable.
 */
export async function forward(
  path: string,
  init?: RequestInit,
): Promise<Response> {
  const base = baseUrl();
  if (!base) return notConfigured();

  let res: Response;
  try {
    res = await fetch(`${base}${path}`, init);
  } catch (err) {
    return Response.json(
      { error: `orchestrator unreachable: ${(err as Error).message}` },
      { status: 502 },
    );
  }

  // 204 (and other empty responses) carry no body; relay as-is.
  const body = res.status === 204 ? null : await res.text();
  return new Response(body, {
    status: res.status,
    headers: {
      "Content-Type":
        res.headers.get("Content-Type") ?? "application/json; charset=utf-8",
    },
  });
}

/**
 * Stream a Server-Sent Events response from `path` on the orchestrator straight
 * through to the caller (unlike `forward`, which buffers the body). The caller's
 * AbortSignal is forwarded, so when the browser closes the EventSource the
 * upstream connection is torn down too. Returns 503 when no orchestrator is
 * configured and 502 when it is unreachable; a non-streamable upstream response
 * is relayed with its status so the client's EventSource sees the error.
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

/**
 * Proxy the incoming editor request to `path` on the orchestrator, preserving the
 * method and (for POST/PUT/PATCH) the JSON body.
 */
export async function proxy(req: Request, path: string): Promise<Response> {
  const method = req.method;
  const init: RequestInit = { method };
  if (method === "POST" || method === "PUT" || method === "PATCH") {
    init.body = await req.text();
    init.headers = { "Content-Type": "application/json" };
  }
  return forward(path, init);
}
