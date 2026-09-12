import type { NextAuthConfig } from "next-auth";
import { keepFresh, signInExchange, type OctoTokenFields } from "@/app/auth/octoToken";
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
 * Edge-safe Auth.js configuration, shared with the full `auth.ts`. One provider,
 * plain authorization-code OIDC, configured entirely from oidc.config.ts.
 *
 * What the provider says about somebody is not what this platform authorizes on.
 * At sign-in the provider's token is traded with iam for a platform token, and
 * the roles on the session come off that — from rows in the platform's own
 * database, not from a claim the provider chose to send. See
 * app/auth/octoToken.ts for the token's life after that.
 */

export const authConfig: NextAuthConfig = {
  trustHost: true,
  // Eight hours rather than the thirty-day default. iam will renew an expired
  // platform token for a short grace period, so an idle session is a credential
  // that can be revived; the session's own lifetime is the only real bound on how
  // long that stays true, and a working day is the honest size for it.
  session: { strategy: "jwt", maxAge: 8 * 60 * 60 },
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
    // The whole session lifecycle, in two calls out to the module that owns it.
    // `account` — not `profile` — is where the provider's raw id token is, and it
    // is present only on sign-in, so the exchange happens once per session and
    // every later call only considers a renewal.
    //
    // Returning null ends the session: Auth.js clears the cookies. Both paths that
    // do it here are deliberate refusals, not errors to be swallowed — a session
    // that cannot get a platform token can call nothing.
    async jwt({ token, account }) {
      if (account?.id_token) {
        const fields = await signInExchange(account.id_token);
        if (!fields) return null;
        return { ...token, ...fields };
      }
      // Cast rather than augment: the JWT interface cannot be augmented from this
      // app (see types/next-auth.d.ts), so the shape is named where it is read.
      const fresh = await keepFresh(token as OctoTokenFields);
      return fresh ? { ...token, ...fresh } : null;
    },
    // Roles and the user id, and pointedly not the token. This object is
    // serialized to the browser at /api/auth/session; see app/auth/octoToken.ts.
    session({ session, token }) {
      const fields = token as OctoTokenFields;
      session.user.roles = fields.roles ?? [];
      if (fields.userId) session.user.id = fields.userId;
      return session;
    },
  },
};
