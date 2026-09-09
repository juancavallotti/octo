/**
 * Is this URL served by our own editor server?
 *
 * One function rather than a comparison at each call site, because the two call
 * sites are a navigation guard and an IPC authorisation check — they have to
 * agree, and when they were written separately they agreed on the wrong thing:
 * both compared with `startsWith`, and a URL may begin with our origin without
 * being served by it.
 *
 *   new URL("http://127.0.0.1:8477@evil.example/x").host  ===  "evil.example"
 *
 * The userinfo before the `@` is not a host. `startsWith("http://127.0.0.1:8477")`
 * is true for that string, so the guard admitted an attacker's page into the
 * window, with the preload bridge attached — and the IPC check then agreed it was
 * the editor. Parsing and comparing the parsed origin is the whole fix.
 */
export function sameOrigin(url: string, origin: string): boolean {
  try {
    return new URL(url).origin === new URL(origin).origin;
  } catch {
    // Not a URL we can parse is not a URL we will trust.
    return false;
  }
}
