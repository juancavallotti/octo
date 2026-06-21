import type { DefaultSession } from "next-auth";

/** Augment Auth.js types with the roles we carry from the OIDC id-token claim. */
declare module "next-auth" {
  interface Session {
    user: {
      roles: string[];
    } & DefaultSession["user"];
  }
}

declare module "next-auth/jwt" {
  interface JWT {
    roles?: string[];
  }
}
