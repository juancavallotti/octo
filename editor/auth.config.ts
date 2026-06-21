import type { NextAuthConfig } from "next-auth";

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

export const authConfig: NextAuthConfig = {
  trustHost: true,
  session: { strategy: "jwt" },
  pages: { signIn: "/auth/signin" },
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
    // Copy the IdP's role claim into the JWT on sign-in so it rides along without
    // a session lookup.
    jwt({ token, profile }) {
      if (profile) {
        token.roles = rolesFrom((profile as Record<string, unknown>)[rolesClaim]);
      }
      return token;
    },
    // Expose roles (and the stable subject) on the session for guards/UI.
    session({ session, token }) {
      session.user.roles = (token.roles as string[] | undefined) ?? [];
      return session;
    },
  },
};
