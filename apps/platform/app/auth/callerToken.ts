/**
 * The platform token to present on the caller's behalf.
 *
 * Two sources, in order: a token set explicitly for the current async context
 * ({@link runWithToken}), then the session cookie. An async-local store rather
 * than a parameter, so that a credential does not appear in the signature of
 * every function that can reach the orchestrator.
 */

import { AsyncLocalStorage } from "node:async_hooks";

const explicit = new AsyncLocalStorage<string>();

/** Run `fn` with `token` as the caller's credential, for the duration of `fn`. */
export function runWithToken<T>(token: string, fn: () => T): T {
  return explicit.run(token, fn);
}

/**
 * The current caller's platform token, or undefined when there is none to
 * present — a request with neither a session nor an explicit token.
 */
export async function callerToken(): Promise<string | undefined> {
  const supplied = explicit.getStore();
  if (supplied) return supplied;
  // Imported lazily: it pulls in `next/server`, which importing this module
  // must not.
  const { currentOctoToken } = await import("./sessionCookie");
  return currentOctoToken();
}
