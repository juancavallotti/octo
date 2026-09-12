import type { DefaultSession } from "next-auth";

/**
 * Augment the Session with what we carry beyond the defaults: the caller's roles
 * and their durable octo user id.
 *
 * Only the Session. The JWT — the encrypted cookie's payload, which also holds
 * the platform token — is deliberately not augmented here, because it cannot be:
 * `next-auth/jwt` is a bare re-export of `@auth/core/jwt`, augmenting a
 * re-export does not reach the interface it forwards, and `@auth/core` is not
 * resolvable from this app under pnpm anyway. A block declared against either
 * one type-checks and does nothing, which is a worse thing to have than no block
 * at all. The callbacks name the shape they read instead; see
 * app/auth/octoToken.ts for what is on it and why the token is not on the
 * Session.
 */
declare module "next-auth" {
  interface Session {
    user: {
      id?: string;
      roles: string[];
    } & DefaultSession["user"];
  }
}
