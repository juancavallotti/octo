import { NextResponse } from "next/server";
import type { NextRequest, ProxyConfig } from "next/server";
import { auth, authEnabled } from "@/auth";
import { hostSet, isIntegrationHost } from "@/app/gateway-error/hostGuard";

/**
 * The part of a request these helpers read. Structural rather than NextRequest so
 * they accept Auth.js's NextAuthRequest too, which is what the gated export hands
 * them — the two differ only by the session it attaches.
 */
type RoutedRequest = Pick<NextRequest, "headers" | "nextUrl">;

/**
 * Next.js proxy (formerly "middleware"): gates the whole editor behind an
 * authenticated session when SSO is configured. Browser navigations without a
 * session are redirected to the sign-in page; unauthenticated `/api/*` calls get a
 * 401. When SSO is not configured (local `task dev`), this is a no-op and the app
 * behaves exactly as before.
 *
 * Per-route role checks live in the route handlers via withAuth (app/auth/guard.ts).
 */

/** Where the gateway's error page lives; see app/gateway-error/route.ts. */
const ERROR_PATH = "/gateway-error";

/**
 * onIntegrationHost reports whether this request came in on the integrations
 * wildcard rather than on the editor's own hostname.
 *
 * The guard exists because the chart points a catch-all route for `*.<baseDomain>`
 * at this service, so an unmatched hostname gets a real page instead of the
 * controller's bare text. That route rewrites the path, but the rewrite is a
 * different annotation on every ingress controller, and a wrong one would quietly
 * publish the editor's sign-in page on the integrations domain. So the app does
 * not rely on it: on one of those hostnames, the error page is the only thing that
 * answers.
 *
 * The decision itself lives in hostGuard.ts, pure and tested — it is wrong in two
 * directions and one of them locks everybody out of the platform. This reads the
 * environment the chart supplies and delegates.
 */
function onIntegrationHost(req: RoutedRequest): boolean {
  return isIntegrationHost(
    req.headers.get("host") ?? "",
    process.env.BASE_DOMAIN,
    hostSet(process.env.PLATFORM_HOSTS),
  );
}

/** Paths reachable without a session (auth endpoints, the welcome page, assets). */
function isPublic(pathname: string): boolean {
  return (
    pathname === "/" ||
    // The gateway's error page. Public by necessity: it is served to whoever typed
    // a URL that goes nowhere, and redirecting them to sign in would replace a
    // clear answer with a confusing one.
    pathname === ERROR_PATH ||
    pathname.startsWith("/api/auth") ||
    // The MCP endpoint authenticates itself with a per-user API key (bearer
    // token), so it must bypass the OIDC session gate — see app/mcp/route.ts.
    pathname === "/mcp" ||
    pathname.startsWith("/mcp/") ||
    // There is deliberately no exemption for a running integration any more. Nothing
    // runs in this pod for longer than a request: a run is a pod of its own, reached at
    // its own hostname, so a webhook callback never passes through here to be redirected
    // to sign-in in the first place.
    pathname === "/octo-logo.png" ||
    pathname === "/icon.png"
  );
}

const gate = auth((req) => {
  const { pathname, search } = req.nextUrl;
  if (onIntegrationHost(req)) return errorPageOnly(req);
  if (req.auth || isPublic(pathname)) return NextResponse.next();
  if (pathname.startsWith("/api/")) {
    return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  }
  // Send unauthenticated browser navigations to the public welcome page, carrying
  // the original path so sign-in can return there.
  const url = new URL("/", req.nextUrl.origin);
  url.searchParams.set("callbackUrl", `${pathname}${search}`);
  return NextResponse.redirect(url);
});

/**
 * errorPageOnly collapses everything on an integration hostname onto the error
 * page. A rewrite rather than a redirect, so the address bar keeps the hostname
 * the person actually typed — which is the thing they need to see to spot a typo.
 */
function errorPageOnly(req: RoutedRequest): NextResponse {
  if (req.nextUrl.pathname === ERROR_PATH) return NextResponse.next();
  const url = req.nextUrl.clone();
  url.pathname = ERROR_PATH;
  url.search = "";
  return NextResponse.rewrite(url);
}

// The integration-host guard applies whether or not SSO is configured: it is about
// which hostname a request arrived on, not about who sent it.
export default authEnabled
  ? gate
  : (req: NextRequest) =>
      onIntegrationHost(req) ? errorPageOnly(req) : NextResponse.next();

export const config: ProxyConfig = {
  // Run on everything except Next internals and static files (which have a dot).
  matcher: ["/((?!_next/static|_next/image|.*\\..*).*)"],
};
