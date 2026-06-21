import NextAuth from "next-auth";
import { authConfig } from "./auth.config";

/**
 * Full Auth.js instance. The config has no database adapter (JWT sessions only),
 * so it is edge-safe and the same instance backs both the route handlers and the
 * middleware. `AUTH_SECRET` is read from the environment automatically.
 */
export const { handlers, auth, signIn, signOut } = NextAuth(authConfig);

export { authEnabled } from "./auth.config";
