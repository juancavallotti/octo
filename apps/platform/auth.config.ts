import type { NextAuthConfig } from "next-auth";
import { bootstrapUser } from "@/app/actions/_client";

/**
 * Edge-safe Auth.js configuration shared by the middleware and the full `auth.ts`.
 * SSO is opt-in: it is only wired up when the OIDC config is present (platform
 * deploys). Local `task dev` runs leave these env vars unset and stay
 * unauthenticated — see `authEnabled`.
 *
 * The identity provider is eetr (`auth.eetr.app`), a generic OIDC provider using
 * the authorization-code flow. Roles are read from a configurable id-token claim
 * (AUTH_ROLES_CLAIM, default "roles") and surfaced on the session for the
 * role-checker guard (app/auth/guard.ts).
 */

/** True when OIDC SSO is configured and should be enforced. */
export const authEnabled =
  !!process.env.AUTH_EETR_ISSUER && !!process.env.AUTH_SECRET;

const rolesClaim = process.env.AUTH_ROLES_CLAIM || "roles";

/** Normalize a roles claim (array, or space/comma-separated string) to a string[]. */
function rolesFrom(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((r): r is string => typeof r === "string");
  }
  if (typeof value === "string") return value.split(/[\s,]+/).filter(Boolean);
  return [];
}

/** Narrow an unknown claim to a non-empty string, or undefined. */
function asString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

export const authConfig: NextAuthConfig = {
  trustHost: true,
  session: { strategy: "jwt" },
  pages: { signIn: "/" },
  providers: [
    {
      id: "eetr",
      name: "eetr",
      type: "oidc",
      issuer: process.env.AUTH_EETR_ISSUER,
      clientId: process.env.AUTH_EETR_CLIENT_ID,
      clientSecret: process.env.AUTH_EETR_CLIENT_SECRET,
      // Request the profile + email scopes so we receive the user's name and
      // picture (mapped to session.user.name / session.user.image by Auth.js).
      authorization: { params: { scope: "openid profile email" } },
    },
  ],
  callbacks: {
    // On sign-in, copy the IdP's role claim into the JWT and bootstrap the user
    // row so both ride along without a per-request lookup. `profile` is present
    // only on sign-in, so the fetch fires once per session, not per request.
    async jwt({ token, profile }) {
      if (profile) {
        const claims = profile as Record<string, unknown>;
        token.roles = rolesFrom(claims[rolesClaim]);
        const subject = token.sub ?? asString(claims.sub);
        const email = asString(claims.email) ?? token.email ?? undefined;
        if (subject && email) {
          // Best-effort: the client never throws (it returns an error result when
          // the orchestrator is unreachable), so a bootstrap failure leaves userId
          // unset rather than blocking sign-in. The API-key actions then surface a
          // clean "user not provisioned" error.
          const res = await bootstrapUser(subject, email, asString(claims.name) ?? "");
          token.userId = res.ok ? res.data.id : undefined;
        }
      }
      return token;
    },
    // Expose roles and the durable user id on the session for guards/UI/actions.
    session({ session, token }) {
      session.user.roles = (token.roles as string[] | undefined) ?? [];
      const userId = token.userId as string | undefined;
      if (userId) session.user.id = userId;
      return session;
    },
  },
};
