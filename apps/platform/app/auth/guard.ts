import { NextResponse } from "next/server";
import type { Session } from "next-auth";
import { auth, authEnabled } from "@/auth";
import { ALL_ROLES } from "./roles";

/**
 * Role-checker for server actions and BFF route handlers. The middleware already
 * requires a session for every request when SSO is enabled; these helpers add the
 * per-route authorization check (and a clean 401/403 for API responses).
 *
 * When SSO is disabled (local `task dev`) every check passes with a synthetic
 * local session, so the app keeps working without an identity provider.
 *
 * Required roles for write operations default to none (any authenticated user) and
 * can be locked down via AUTH_WRITE_ROLES (comma-separated) without code changes.
 */

/** Roles permitted to perform write/mutating operations; empty = any signed-in user. */
export const writeRoles = (process.env.AUTH_WRITE_ROLES ?? "")
  .split(",")
  .map((r) => r.trim())
  .filter(Boolean);

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
