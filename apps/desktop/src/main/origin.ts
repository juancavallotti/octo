/**
 * Is this URL served by our own editor server?
 *
 * Compares parsed origins, never string prefixes: the userinfo before an `@` is
 * not a host, so `http://127.0.0.1:8477@evil.example/x` begins with our origin and
 * is served by `evil.example`.
 */
export function sameOrigin(url: string, origin: string): boolean {
  try {
    return new URL(url).origin === new URL(origin).origin;
  } catch {
    // Not a URL we can parse is not a URL we will trust.
    return false;
  }
}
