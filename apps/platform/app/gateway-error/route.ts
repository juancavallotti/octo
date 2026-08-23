import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";
import { cleanHost, explanationFor, renderErrorPage } from "./errorPage";

/**
 * The page a request gets when it never reached an integration.
 *
 * Two different things route here, and they arrive differently:
 *
 *   - **A hostname nothing is deployed at.** The chart points a catch-all route
 *     for `*.<baseDomain>` at this service, rewriting the path here. Nothing tells
 *     us a status, so it is a 404 — which is what it is.
 *   - **A deployment that is down.** ingress-nginx and Traefik can be told to fetch
 *     their error pages from a backend instead of writing their own. They send the
 *     original status in `X-Code`, which is why that header is honoured rather than
 *     always answering 404.
 *
 * The status is echoed back on the response, not just printed in the body: an
 * uptime check that saw HTTP 200 with the word "503" on it would be worse than the
 * bare gateway text this replaces.
 *
 * It is a route handler rather than a page because a page cannot choose its own
 * status code, and because this must not depend on the React runtime, a stylesheet
 * or a font to render — see errorPage.ts.
 */

/** Statuses this page will serve. Anything else is not ours to speak for. */
const ALLOWED = new Set([400, 403, 404, 408, 500, 502, 503, 504]);

/** What a request with no status attached is: nothing is deployed there. */
const DEFAULT_STATUS = 404;

/**
 * PLATFORM_URL is where the "go to the platform" link points. It comes from the
 * chart, never from the request: a link built out of a Host header would let
 * whoever sent the request choose where this page sends people next.
 */
function platformURL(): string {
  const raw = (process.env.PLATFORM_URL ?? "").trim();
  if (!raw) return "";
  try {
    const url = new URL(raw);
    // Anything but http(s) — javascript:, data: — is not a link this page offers.
    return url.protocol === "https:" || url.protocol === "http:" ? url.toString() : "";
  } catch {
    return "";
  }
}

/** resolveStatus reads the controller's X-Code, falling back to a plain 404. */
function resolveStatus(req: NextRequest): number {
  const raw = req.headers.get("x-code");
  if (!raw) return DEFAULT_STATUS;
  const code = Number.parseInt(raw.trim(), 10);
  return ALLOWED.has(code) ? code : DEFAULT_STATUS;
}

/**
 * resolveHost finds the hostname the caller actually asked for.
 *
 * `X-Original-Host`/`X-Forwarded-Host` carry it when a proxy has rewritten Host on
 * the way in; `?host=` is how the ALB path passes it, since ALB can substitute the
 * host into a redirect but cannot add a header. Host itself is the common case.
 * Every source is equally untrusted, and cleanHost is what makes that safe.
 */
function resolveHost(req: NextRequest): string {
  const candidates = [
    req.nextUrl.searchParams.get("host"),
    req.headers.get("x-original-host"),
    req.headers.get("x-forwarded-host"),
    req.headers.get("host"),
  ];
  for (const candidate of candidates) {
    const host = cleanHost(candidate);
    if (host) return host;
  }
  return "";
}

/**
 * wantsJSON reports whether the caller asked for something other than a page.
 *
 * Most traffic to a deployment's hostname is not a browser — it is a webhook or an
 * API client, and handing one an HTML document in place of the terse error it
 * expected makes a bad log line worse. ingress-nginx forwards the original Accept
 * in `X-Format` precisely so a backend can make this choice.
 */
function wantsJSON(req: NextRequest): boolean {
  const accept = (
    req.headers.get("x-format") ??
    req.headers.get("accept") ??
    ""
  ).toLowerCase();
  if (accept.includes("text/html")) return false;
  return accept.includes("json") || accept.includes("*/*") === false;
}

function respond(req: NextRequest): NextResponse {
  const status = resolveStatus(req);
  const host = resolveHost(req);

  if (wantsJSON(req)) {
    const { title, body } = explanationFor(status);
    return NextResponse.json(
      { error: title, detail: body, status, host: host || undefined },
      { status, headers: { "Cache-Control": "no-store" } },
    );
  }

  return new NextResponse(
    renderErrorPage({ status, host, platformURL: platformURL() }),
    {
      status,
      headers: {
        "Content-Type": "text/html; charset=utf-8",
        // Never cached: the same address answers differently the moment somebody
        // deploys to it, and a cached 404 would outlive the thing it described.
        "Cache-Control": "no-store",
      },
    },
  );
}

export const dynamic = "force-dynamic";

export function GET(req: NextRequest): NextResponse {
  return respond(req);
}

/**
 * Every other method answers the same way. A webhook POSTing to a hostname whose
 * deployment is gone should get the explanation too, not a 405 that sends its
 * author looking for the wrong problem.
 */
export const POST = GET;
export const PUT = GET;
export const PATCH = GET;
export const DELETE = GET;
export const HEAD = GET;
export const OPTIONS = GET;
