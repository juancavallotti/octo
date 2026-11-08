/**
 * The environment to act in on the caller's behalf.
 *
 * Two sources, in order, mirroring callerToken: one set explicitly for the
 * current async context ({@link runWithEnvironment}), then the cookie the
 * switcher writes. Empty is the default environment, and is what a caller with
 * neither gets — which is also what every request made before environments
 * existed meant.
 */

import { AsyncLocalStorage } from "node:async_hooks";
import { ENVIRONMENT_COOKIE, normalizeEnvironment } from "@/app/lib/environment";

const explicit = new AsyncLocalStorage<string>();

/** Run `fn` acting in `environment`, for the duration of `fn`. */
export function runWithEnvironment<T>(environment: string, fn: () => T): T {
  return explicit.run(normalizeEnvironment(environment), fn);
}

/** The current caller's environment, "" for the default. */
export async function callerEnvironment(): Promise<string> {
  const supplied = explicit.getStore();
  if (supplied !== undefined) return supplied;
  try {
    // Imported lazily, like the session cookie: importing this module must not
    // pull in next/headers, and outside a request there are no cookies to read.
    const { cookies } = await import("next/headers");
    const jar = await cookies();
    return normalizeEnvironment(jar.get(ENVIRONMENT_COOKIE)?.value);
  } catch {
    return "";
  }
}
