import { cookies } from "next/headers";
import {
  NAMESPACE_COOKIE,
  NAMESPACE_MAX_AGE_SECONDS,
  deriveNamespace,
  isValidNamespace,
  newNamespace,
} from "@octo/run-host";

/**
 * Resolve the run namespace for a caller: this browser's cookie (minted and set
 * when absent) mixed with the calling tab's id, so each tab drives its own runner.
 * The server-action counterpart to run-host's request-based `ensureNamespace(req)`,
 * which the SSE log stream uses to reach the same namespace.
 *
 * `tabId` arrives from the client and is therefore untrusted; `deriveNamespace`
 * both validates it and keeps the cookie — which the client cannot read — as the
 * half that decides *whose* runners are reachable at all.
 */
export async function ensureRunNamespace(tabId?: string): Promise<string> {
  const jar = await cookies();
  const existing = jar.get(NAMESPACE_COOKIE)?.value;
  if (existing && isValidNamespace(existing)) return deriveNamespace(existing, tabId);
  const ns = newNamespace();
  jar.set(NAMESPACE_COOKIE, ns, {
    path: "/",
    httpOnly: true,
    sameSite: "lax",
    maxAge: NAMESPACE_MAX_AGE_SECONDS,
  });
  return deriveNamespace(ns, tabId);
}
