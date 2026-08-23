import type { NextAuthConfig } from "next-auth";
import { bootstrapUser } from "@/app/actions/_client";
import {
  OIDC_AUTHORIZATION_URL,
  OIDC_CLIENT_ID,
  OIDC_CLIENT_SECRET,
  OIDC_ISSUER,
  OIDC_JWKS_URL,
  OIDC_PROVIDER_ID,
  OIDC_PROVIDER_NAME,
  OIDC_SCOPES,
  OIDC_TOKEN_URL,
  OIDC_USERINFO_URL,
} from "@/oidc.config";

/**
 * Edge-safe Auth.js configuration, shared with the full `auth.ts`.
 * SSO is opt-in: it is only wired up when the OIDC config is present (platform
 * deploys). Local `task dev` runs leave these env vars unset and stay
 * unauthenticated — see `authEnabled`.
 *
 * The identity provider is whatever OIDC provider the operator configured (see
 * oidc.config.ts); Octo talks plain authorization-code OIDC and privileges none
 * of them. Roles are read from a configurable id-token claim (AUTH_ROLES_CLAIM,
 * default "roles") and surfaced on the session for the role-checker guard
 * (app/auth/guard.ts).
 */

/** True when OIDC SSO is configured and should be enforced. */
export const authEnabled = !!OIDC_ISSUER && !!process.env.AUTH_SECRET;

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
      id: OIDC_PROVIDER_ID,
      name: OIDC_PROVIDER_NAME,
      type: "oidc",
      issuer: OIDC_ISSUER,
      clientId: OIDC_CLIENT_ID,
      clientSecret: OIDC_CLIENT_SECRET,
      // Everything below the scopes is an escape hatch for providers whose
      // discovery document does not answer for them. The keys are spread in only
      // when set: an explicit `undefined` reads as "no endpoint" to Auth.js
      // rather than "discover it", which would break the compliant case.
      authorization: {
        ...(OIDC_AUTHORIZATION_URL ? { url: OIDC_AUTHORIZATION_URL } : {}),
        params: { scope: OIDC_SCOPES },
      },
      ...(OIDC_TOKEN_URL ? { token: OIDC_TOKEN_URL } : {}),
      ...(OIDC_USERINFO_URL ? { userinfo: OIDC_USERINFO_URL } : {}),
      ...(OIDC_JWKS_URL ? { jwks_endpoint: OIDC_JWKS_URL } : {}),
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
        // Key the user on the IdP's real OIDC subject (`profile.sub`), NOT
        // Auth.js's `token.sub`. With the JWT session strategy Auth.js mints its
        // own `token.sub` per sign-in, so preferring it created a fresh user row
        // every login (and disagreed with the /mcp path, which keys on the access
        // token's `sub`). `claims.sub` is the stable identifier both paths share.
        const subject = asString(claims.sub) ?? token.sub;
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
