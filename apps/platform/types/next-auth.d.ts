import type { DefaultSession } from "next-auth";

/**
 * Augment Auth.js types with what we carry beyond the defaults: the roles from the
 * OIDC id-token claim, and the durable orchestrator user id bootstrapped on
 * sign-in (used to scope per-user data such as API keys).
 */
declare module "next-auth" {
  interface Session {
    user: {
      id?: string;
      roles: string[];
    } & DefaultSession["user"];
  }
}

declare module "next-auth/jwt" {
  interface JWT {
    roles?: string[];
    userId?: string;
  }
}
