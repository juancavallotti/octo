import { NextResponse } from "next/server";
import type { Session } from "next-auth";
import { auth } from "@/auth";
import { PLATFORM_ADMIN, PLATFORM_DEVELOPER, PLATFORM_OPERATOR } from "./roles";

/**
 * Role checks for server actions and route handlers. Each requires a session and,
 * optionally, one of a set of roles; AuthError means no session, ForbiddenError
 * means the session lacks the roles. There is no path through these that passes
 * without a session.
 */

/**
 * Roles permitted to perform write and mutating operations.
 *
 * Defaults to every role except `platform:monitor`, the role that means "looks,
 * and nothing else". AUTH_WRITE_ROLES replaces the list outright; an empty value
 * reads as unset rather than as "nobody", so clearing the variable cannot take
 * writes away from everyone.
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

/** Require an authenticated session, or throw AuthError. */
export async function requireSession(): Promise<Session> {
  const session = await auth();
  if (!session?.user) throw new AuthError("unauthenticated");
  return session;
}

/** Require a session holding at least one of `roles` (no roles = session only). */
export async function requireRole(...roles: string[]): Promise<Session> {
  const session = await requireSession();
  if (roles.length === 0) return session;
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
