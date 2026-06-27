import { randomBytes } from "node:crypto";

/**
 * Per-user run namespaces. The editor's RUN feature is multi-user: each browser
 * gets an 8-char namespace slug carried in an HttpOnly cookie, and the session
 * manager keys every running `octo` process by that slug. A cookie (not
 * localStorage) is used so the SSE log stream and the `/editor/runs/<ns>/` reverse
 * proxy — both plain server requests that can't set custom headers — carry the
 * identity automatically.
 */

/** Cookie name holding the run namespace slug. Exported so an app that mints the
 * cookie from a server action (via its framework's own cookie store) stays in
 * sync with what {@link readNamespace} and the SSE/proxy routes expect. */
export const NAMESPACE_COOKIE = "octo_ns";
const COOKIE = NAMESPACE_COOKIE;

/** Slug alphabet/shape: lowercase alphanumerics only, so it is safe as both a
 * filesystem directory name and a URL path segment. */
const SLUG_RE = /^[a-z0-9]{8}$/;
const ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";

/** Keep the namespace cookie around across reloads; idle processes are reaped
 * server-side, so the identity can outlive any single run. Exported alongside
 * {@link NAMESPACE_COOKIE} for server-action cookie minting. */
export const NAMESPACE_MAX_AGE_SECONDS = 7 * 24 * 60 * 60;
const MAX_AGE_SECONDS = NAMESPACE_MAX_AGE_SECONDS;

/** isValidNamespace reports whether a slug is well-formed (used to validate a
 * namespace taken from a URL path before it reaches the session manager). */
export function isValidNamespace(ns: string): boolean {
  return SLUG_RE.test(ns);
}

/** newNamespace mints a random 8-char slug. */
export function newNamespace(): string {
  const bytes = randomBytes(8);
  let out = "";
  for (let i = 0; i < bytes.length; i++) out += ALPHABET[bytes[i] % ALPHABET.length];
  return out;
}

/** readNamespace returns the request's namespace slug, or null when absent or
 * malformed (the latter guards against a tampered cookie reaching the filesystem
 * or proxy path). */
export function readNamespace(req: Request): string | null {
  const header = req.headers.get("cookie");
  if (!header) return null;
  for (const part of header.split(";")) {
    const eq = part.indexOf("=");
    if (eq < 0) continue;
    if (part.slice(0, eq).trim() !== COOKIE) continue;
    const value = part.slice(eq + 1).trim();
    return SLUG_RE.test(value) ? value : null;
  }
  return null;
}

/** cookieHeader renders the Set-Cookie value for a freshly minted namespace. */
function cookieHeader(ns: string): string {
  return `${COOKIE}=${ns}; Path=/; HttpOnly; SameSite=Lax; Max-Age=${MAX_AGE_SECONDS}`;
}

/** ensureNamespace returns the request's namespace, minting one (with the
 * Set-Cookie header to attach to the response) when the request has none. */
export function ensureNamespace(req: Request): { ns: string; setCookie?: string } {
  const existing = readNamespace(req);
  if (existing) return { ns: existing };
  const ns = newNamespace();
  return { ns, setCookie: cookieHeader(ns) };
}
