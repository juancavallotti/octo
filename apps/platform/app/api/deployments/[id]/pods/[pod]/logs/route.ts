export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string; pod: string }> };

/** The orchestrator base URL with any trailing slash trimmed, or "" when unset. */
function baseUrl(): string {
  return (process.env.ORCHESTRATOR_URL ?? "").replace(/\/+$/, "");
}

/**
 * GET /api/deployments/:id/pods/:pod/logs — proxies the orchestrator's pod-log
 * stream to the browser as a chunked text/plain body. With ?follow=1 the
 * orchestrator tails the pod live and holds the connection open; the browser's
 * fetch abort (on unmount) propagates here via req.signal, which cancels the
 * upstream fetch and the k8s stream behind it. 503 when the orchestrator is
 * unconfigured, so the client can surface a clear message.
 */
export async function GET(req: Request, { params }: Params) {
  const { id, pod } = await params;
  const base = baseUrl();
  if (!base) {
    return Response.json(
      { error: "orchestrator not configured (ORCHESTRATOR_URL unset)" },
      { status: 503 },
    );
  }

  const follow = new URL(req.url).searchParams.get("follow") ?? "1";
  const url =
    `${base}/deployments/${encodeURIComponent(id)}` +
    `/pods/${encodeURIComponent(pod)}/logs?follow=${encodeURIComponent(follow)}`;

  let upstream: Response;
  try {
    upstream = await fetch(url, {
      signal: req.signal,
      // Never let fetch buffer the whole (potentially endless) follow stream.
      cache: "no-store",
    });
  } catch (e) {
    // An aborted request (client navigated away) is expected; report anything else.
    if ((e as Error).name === "AbortError") return new Response(null, { status: 499 });
    return Response.json({ error: (e as Error).message }, { status: 502 });
  }

  if (!upstream.ok || !upstream.body) {
    const text = await upstream.text().catch(() => "");
    return Response.json(
      { error: text || `pod logs unavailable (${upstream.status})` },
      { status: upstream.status || 502 },
    );
  }

  return new Response(upstream.body, {
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      "Cache-Control": "no-cache, no-transform",
      "X-Accel-Buffering": "no",
    },
  });
}
