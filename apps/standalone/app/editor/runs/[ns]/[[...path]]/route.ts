import { runningPort, isValidNamespace } from "@octo/run-host";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/**
 * Reverse proxy for testing a running networked integration from the editor.
 * `/editor/runs/<ns>/<path>` forwards to the namespace's octo process on
 * 127.0.0.1:<allocatedPort>, so a user can hit their integration's HTTP endpoints
 * without exposing the port. The target port is resolved server-side from the
 * namespace (in the URL), so the path is the same regardless of which port the run
 * landed on. HTTP only — Next route handlers can't upgrade WebSockets.
 */

type Params = { params: Promise<{ ns: string; path?: string[] }> };

/** Headers that are connection-specific and must not be forwarded. */
const HOP_BY_HOP = new Set([
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
  "host",
]);

function forwardableRequestHeaders(headers: Headers): Headers {
  const out = new Headers();
  headers.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase())) out.set(key, value);
  });
  return out;
}

function forwardableResponseHeaders(headers: Headers): Headers {
  const out = new Headers();
  headers.forEach((value, key) => {
    const k = key.toLowerCase();
    // undici has already decoded the body, so the upstream content-encoding/length
    // no longer describe what we send; drop them and let the platform recompute.
    if (HOP_BY_HOP.has(k) || k === "content-encoding" || k === "content-length")
      return;
    out.set(key, value);
  });
  return out;
}

async function proxy(req: Request, { params }: Params): Promise<Response> {
  const { ns, path } = await params;
  if (!isValidNamespace(ns)) {
    return new Response("invalid run namespace", { status: 404 });
  }
  const port = runningPort(ns);
  if (port === null) {
    return new Response("no running networked integration for this run", {
      status: 404,
    });
  }

  const search = new URL(req.url).search;
  const suffix = (path ?? []).map(encodeURIComponent).join("/");
  const target = `http://127.0.0.1:${port}/${suffix}${search}`;

  const method = req.method;
  const hasBody = method !== "GET" && method !== "HEAD";
  const init: RequestInit & { duplex?: "half" } = {
    method,
    headers: forwardableRequestHeaders(req.headers),
    redirect: "manual",
  };
  if (hasBody) {
    init.body = req.body;
    init.duplex = "half"; // stream the request body to the upstream
  }

  let upstream: Response;
  try {
    upstream = await fetch(target, init);
  } catch (err) {
    return new Response(`run proxy error: ${(err as Error).message}`, {
      status: 502,
    });
  }

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: forwardableResponseHeaders(upstream.headers),
  });
}

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
export const HEAD = proxy;
export const OPTIONS = proxy;
