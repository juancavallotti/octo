/** Valid POSIX-ish env var name: a letter or underscore, then word chars. */
const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;

/**
 * Validate a dev-env map for a run: a plain object of valid env names to string
 * values. Returns the sanitized map, or null if the shape is invalid. Mirrors the
 * hosts' `parseDevEnv` (see each app's run action) so MCP-driven runs accept the
 * same env shape as the editor's "Dev .env".
 */
export function parseEnv(value: unknown): Record<string, string> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return null;
  }
  const out: Record<string, string> = {};
  for (const [name, val] of Object.entries(value as Record<string, unknown>)) {
    if (!ENV_NAME.test(name) || typeof val !== "string") return null;
    out[name] = val;
  }
  return out;
}
