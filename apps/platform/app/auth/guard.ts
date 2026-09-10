import { NextResponse } from "next/server";
import type { Session } from "next-auth";
import { auth, authEnabled } from "@/auth";
import {
  ALL_ROLES,
  PLATFORM_ADMIN,
  PLATFORM_DEVELOPER,
  PLATFORM_OPERATOR,
} from "./roles";

/**
 * Role-checker for server actions and BFF route handlers. The middleware already
 * requires a session for every request when SSO is enabled; these helpers add the
 * per-route authorization check (and a clean 401/403 for API responses).
 *
 * When SSO is disabled (local `task dev`) every check passes with a synthetic
 * local session, so the app keeps working without an identity provider.
 *
 * Writes require one of the roles that describe somebody who builds or runs
 * things here. AUTH_WRITE_ROLES narrows that further without a code change.
 */

/**
 * Roles permitted to perform write and mutating operations.
 *
 * Everything except `platform:monitor`, which is the role that exists to mean
 * "looks, and nothing else". This used to default to an empty list, which meant
 * any signed-in user could write — a default the documentation apologised for in
 * three places. It is only safe to change now because roles are rows an
 * administrator can grant, rather than a claim they would have had to go and
 * edit at their identity provider.
 *
 * AUTH_WRITE_ROLES replaces the list outright, for an installation that wants to
 * narrow it further. An empty value is treated as unset rather than as "nobody":
 * a variable somebody cleared should not silently take writes away from everyone.
 */
export const writeRoles = ((): string[] => {
  const configured = (process.env.AUTH_WRITE_ROLES ?? "")
    .split(",")
    .map((r) => r.trim())
    .filter(Boolean);
  return configured.length > 0
    ? configured
    : [PLATFORM_ADMIN, PLATFORM_OPERATOR, PLATFORM_DEVELOPER];
})();

export class AuthError extends Error {} // → 401
export class ForbiddenError extends Error {} // → 403

// Every role, not none. `requireRole` short-circuits before looking at these when
// SSO is off, so they change nothing server-side — but the same session feeds the
// roles context, and an empty list there would hide the admin section from a local
// `task dev` run that can in fact use it.
const LOCAL_SESSION: Session = {
  user: { roles: [...ALL_ROLES] },
  expires: "",
} as Session;

/** Require an authenticated session, or throw AuthError. */
export async function requireSession(): Promise<Session> {
  if (!authEnabled) return LOCAL_SESSION;
  const session = await auth();
  if (!session?.user) throw new AuthError("unauthenticated");
  return session;
}

/** Require a session holding at least one of `roles` (no roles = session only). */
export async function requireRole(...roles: string[]): Promise<Session> {
  const session = await requireSession();
  if (!authEnabled || roles.length === 0) return session;
  const have = new Set(session.user.roles ?? []);
  if (!roles.some((r) => have.has(r))) throw new ForbiddenError("forbidden");
  return session;
}

type RouteHandler<C> = (req: Request, ctx: C) => Promise<Response> | Response;

/**
 * Wrap a route handler so it runs only for an authenticated (and, if `roles` are
 * given, suitably authorized) caller. AuthError → 401, ForbiddenError → 403.
 */
export function withAuth<C>(
  handler: RouteHandler<C>,
  opts?: { roles?: string[] },
): RouteHandler<C> {
  return async (req, ctx) => {
    try {
      await requireRole(...(opts?.roles ?? []));
    } catch (err) {
      if (err instanceof ForbiddenError) {
        return NextResponse.json({ error: "forbidden" }, { status: 403 });
      }
      if (err instanceof AuthError) {
        return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
      }
      throw err;
    }
    return handler(req, ctx);
  };
}
