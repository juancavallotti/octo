/**
 * Whether a request arrived on the wildcard domain integrations are published
 * under, rather than on the editor's own hostname.
 *
 * Its own module, and pure, because the decision is load-bearing in both
 * directions and each direction is a different kind of bad:
 *
 *   - Say NO when it should say yes, and the editor's sign-in page is published on
 *     the integrations domain, reachable at any hostname nobody deployed to.
 *   - Say YES when it should say no, and the editor serves the gateway error page
 *     to itself and nobody can reach the platform at all.
 *
 * The second is not hypothetical: the editor may legitimately live *under* the
 * wildcard — ingress.host=octo.apps.example.com with baseDomain=apps.example.com
 * is an ordinary layout, and nothing in the chart forbids it — so the platform's
 * own hostnames have to be excluded explicitly. The chart passes them in.
 */

/** Lowercase a Host header value and drop its port. */
function normalizeHost(raw: string): string {
  return raw.split(":")[0].trim().toLowerCase();
}

/** Split a comma-separated hostname list into a normalized set. */
export function hostSet(raw: string | undefined): Set<string> {
  return new Set(
    (raw ?? "")
      .split(",")
      .map(normalizeHost)
      .filter(Boolean),
  );
}

export function isIntegrationHost(
  rawHost: string,
  rawBaseDomain: string | undefined,
  platformHosts: Set<string>,
): boolean {
  const base = normalizeHost(rawBaseDomain ?? "");
  // No wildcard domain configured — outside a cluster, there is nothing for a
  // hostname to be confused with, so the guard is off entirely.
  if (!base) return false;

  const host = normalizeHost(rawHost);
  if (!host) return false;
  // The editor's own hostnames are never integration hosts, even under the wildcard.
  if (platformHosts.has(host)) return false;
  // A subdomain of the base, not the base itself: the apex is where something like
  // a marketing site or the editor may sit, and it is not a published endpoint.
  return host.endsWith(`.${base}`);
}
