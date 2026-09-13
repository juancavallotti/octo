import { createHmac, randomBytes } from "node:crypto";

/**
 * Per-tab run namespaces. A running `octo` process is keyed by an 8-char namespace
 * slug, and two browser tabs sharing one fight over a single runner, so a namespace
 * is composed of two halves:
 *
 *   - the *identity* half, an 8-char slug in an HttpOnly cookie. A cookie rather than
 *     localStorage because a plain browser request (an SSE stream) can set no custom
 *     headers, and HttpOnly keeps it unreadable, and so unforgeable, from script.
 *   - the *tab* half, an opaque id the browser keeps in sessionStorage — per-tab by
 *     definition, stable across reloads — and sends up with each call.
 *
 * {@link deriveNamespace} mixes them. Because the cookie half never leaves the server,
 * a client that lies about its tab id only ever reaches another namespace of its own,
 * never another user's. A derived namespace is therefore also usable on its own as an
 * unguessable token.
 */

/** Cookie name holding the run namespace slug. Exported so a caller that mints the
 * cookie itself stays in sync with what {@link readNamespace} expects. */
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

/** Shape of the per-tab id a browser sends up. Deliberately wider than the slug
 * alphabet (the editor mints a UUID's hex) but bounded, so a garbage or oversized
 * value is rejected rather than hashed. */
const TAB_ID_RE = /^[A-Za-z0-9_-]{8,64}$/;

/** isValidTabId reports whether a client-supplied tab id is well-formed. */
export function isValidTabId(tab: string): boolean {
  return TAB_ID_RE.test(tab);
}

/** Largest multiple of the alphabet size that fits in a byte; bytes at or above it
 * are rejected rather than folded, so every slug character is drawn uniformly. */
const REJECT_AT = 256 - (256 % ALPHABET.length);

/**
 * deriveNamespace mixes a browser's cookie namespace with one of its tabs' ids to
 * get that tab's own namespace, in the same 8-char slug shape (so it stays valid as
 * both a directory name and a URL segment).
 *
 * The cookie namespace is the secret and keys the HMAC, which is what makes the
 * result unguessable by anyone who doesn't already hold the cookie.
 *
 * An absent or malformed tab id yields the cookie namespace unchanged, so a caller
 * with no tab to name still gets a namespace of its own. Every caller may hand this a
 * raw untrusted value.
 */
export function deriveNamespace(base: string, tab: string | null | undefined): string {
  if (!tab || !isValidTabId(tab)) return base;
  // One 32-byte digest all but always yields the 8 characters (a byte is rejected
  // ~1.6% of the time), but "all but always" is not "always" — keep drawing from
  // further rounds so the result is guaranteed to be a full, valid slug.
  let out = "";
  for (let round = 0; out.length < 8; round++) {
    const digest = createHmac("sha256", base).update(`${tab}:${round}`).digest();
    for (let i = 0; i < digest.length && out.length < 8; i++) {
      if (digest[i] >= REJECT_AT) continue;
      out += ALPHABET[digest[i] % ALPHABET.length];
    }
  }
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
