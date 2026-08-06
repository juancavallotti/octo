import { NextResponse } from "next/server";
import type { ProxyConfig } from "next/server";
import { auth, authEnabled } from "@/auth";

/**
 * Next.js proxy (formerly "middleware"): gates the whole editor behind an
 * authenticated session when SSO is configured. Browser navigations without a
 * session are redirected to the sign-in page; unauthenticated `/api/*` calls get a
 * 401. When SSO is not configured (local `task dev`), this is a no-op and the app
 * behaves exactly as before.
 *
 * Per-route role checks live in the route handlers via withAuth (app/auth/guard.ts).
 */

/** Paths reachable without a session (auth endpoints, the welcome page, assets). */
function isPublic(pathname: string): boolean {
  return (
    pathname === "/" ||
    pathname.startsWith("/api/auth") ||
    // The MCP endpoint authenticates itself with a per-user API key (bearer
    // token), so it must bypass the OIDC session gate — see app/mcp/route.ts.
    pathname === "/mcp" ||
    pathname.startsWith("/mcp/") ||
    // The run proxy (app/editor/runs/[ns]/…) lets a caller hit an integration
    // running in this pod — now only one started through /mcp, since the editor's
    // Run has its own public host. The run namespace in the URL is an unguessable
    // token and the target is a local process, so — like /mcp — it must bypass the
    // OIDC session gate; otherwise every run URL (including webhook callbacks) is
    // redirected to sign-in.
    pathname.startsWith("/editor/runs/") ||
    pathname === "/octo-logo.png" ||
    pathname === "/icon.png"
  );
}

const gate = auth((req) => {
  const { pathname, search } = req.nextUrl;
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

export default authEnabled ? gate : () => NextResponse.next();

export const config: ProxyConfig = {
  // Run on everything except Next internals and static files (which have a dot).
  matcher: ["/((?!_next/static|_next/image|.*\\..*).*)"],
};
