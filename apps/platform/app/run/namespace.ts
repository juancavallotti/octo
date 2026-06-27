import { cookies } from "next/headers";
import {
  NAMESPACE_COOKIE,
  NAMESPACE_MAX_AGE_SECONDS,
  isValidNamespace,
  newNamespace,
} from "@octo/run-host";

/**
 * Read this browser's run namespace from its cookie, minting and setting one when
 * absent — the server-action counterpart to run-host's request-based
 * `ensureNamespace(req)`. The cookie name/lifetime come from run-host so the SSE
 * log stream and the `/editor/runs/<ns>/` reverse proxy (which read the same
 * cookie) stay in sync.
 */
export async function ensureRunNamespace(): Promise<string> {
  const jar = await cookies();
  const existing = jar.get(NAMESPACE_COOKIE)?.value;
  if (existing && isValidNamespace(existing)) return existing;
  const ns = newNamespace();
  jar.set(NAMESPACE_COOKIE, ns, {
    path: "/",
    httpOnly: true,
    sameSite: "lax",
    maxAge: NAMESPACE_MAX_AGE_SECONDS,
  });
  return ns;
}
