/**
 * The platform token to present on the caller's behalf, wherever the call is
 * coming from.
 *
 * Two kinds of caller reach the orchestrator client, and they carry their
 * identity differently. A page or a server action has an Auth.js session, and the
 * token is in its cookie. The `/mcp` endpoint has no session at all — it
 * authenticated its caller with a bearer and exchanged it — so it puts the token
 * here for the duration of the request instead.
 *
 * An async-local store rather than an argument, because the alternative is a
 * credential in the signature of a hundred domain functions, every one of which
 * would then be a place it could be logged.
 */

import { AsyncLocalStorage } from "node:async_hooks";

const explicit = new AsyncLocalStorage<string>();

/**
 * Run `fn` with `token` as the caller's credential. For a request that has no
 * session to read one from.
 */
export function runWithToken<T>(token: string, fn: () => T): T {
  return explicit.run(token, fn);
}

/**
 * The current caller's platform token, or undefined when there is none to
 * present — an unconfigured install, or a request with neither a session nor an
 * explicit token.
 */
export async function callerToken(): Promise<string | undefined> {
  const supplied = explicit.getStore();
  if (supplied) return supplied;
  if (!ssoConfigured()) return undefined;
  // Imported here rather than at the top, and only once we know there is a
  // session to read. Reading one pulls in Auth.js, which pulls in `next/server`,
  // and that should not be a dependency of every module that happens to call the
  // orchestrator — nor something a unit test has to stand up to exercise one.
  const { currentOctoToken } = await import("./sessionCookie");
  return currentOctoToken();
}

/**
 * Whether this install has single sign-on, read straight from the environment.
 *
 * The same condition `authEnabled` reports, deliberately not imported from it:
 * importing `@/auth` to ask the question would load the very thing this is
 * avoiding loading. Two spellings of one fact is a small price for the import
 * graph staying honest, and the fact is two environment variables.
 */
function ssoConfigured(): boolean {
  return !!process.env.OIDC_ISSUER && !!process.env.AUTH_SECRET;
}
