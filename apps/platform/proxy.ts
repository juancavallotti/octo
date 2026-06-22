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

/** Paths reachable without a session (auth endpoints, the sign-in page, assets). */
function isPublic(pathname: string): boolean {
  return (
    pathname.startsWith("/api/auth") ||
    pathname.startsWith("/auth/signin") ||
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
  const url = new URL("/auth/signin", req.nextUrl.origin);
  url.searchParams.set("callbackUrl", `${pathname}${search}`);
  return NextResponse.redirect(url);
});

export default authEnabled ? gate : () => NextResponse.next();

export const config: ProxyConfig = {
  // Run on everything except Next internals and static files (which have a dot).
  matcher: ["/((?!_next/static|_next/image|.*\\..*).*)"],
};
