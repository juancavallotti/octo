/**
 * The platform's roles, mirroring iam/internal/user/roles.go.
 *
 * Four of them, namespaced with `platform:` so that per-integration or per-folder
 * grants can be added later without colliding. The duplication across languages
 * is unavoidable — one side is Go, the other TypeScript — and it is kept small on
 * purpose: only the strings this app names in code are here. The catalogue a
 * person picks from in the admin UI is fetched from iam's GET /roles, so a role
 * added there shows up without a change here.
 */

export const PLATFORM_ADMIN = "platform:admin";
export const PLATFORM_MONITOR = "platform:monitor";
export const PLATFORM_DEVELOPER = "platform:developer";
export const PLATFORM_OPERATOR = "platform:operator";
/** A pipeline's API key: ships integrations. Carried by keys only, never held by a person. */
export const PLATFORM_CICD = "platform:cicd";

/** Short labels for the roles, for anywhere one is shown to a person. */
export const ROLE_LABELS: Record<string, string> = {
  [PLATFORM_ADMIN]: "Admin",
  [PLATFORM_OPERATOR]: "Operator",
  [PLATFORM_DEVELOPER]: "Developer",
  [PLATFORM_MONITOR]: "Monitor",
  [PLATFORM_CICD]: "CI/CD",
};

/**
 * Whether somebody holding `held` may hand `role` on to a key, mirroring
 * MayDelegate in iam/internal/user/roles.go: they hold it, or they are an admin.
 * CI/CD is held by nobody, and is delegated by an admin or by somebody holding
 * both developer and operator. Roles are facets, not a ladder. iam decides; this
 * only keeps a person from being offered a role it would refuse.
 */
export function mayDelegate(held: readonly string[], role: string): boolean {
  if (held.includes(PLATFORM_ADMIN)) return true;
  if (role === PLATFORM_CICD) {
    return held.includes(PLATFORM_DEVELOPER) && held.includes(PLATFORM_OPERATOR);
  }
  return held.includes(role);
}

/** The roles an API key may carry, in presentation order: never admin. */
export const KEY_ROLES = [
  PLATFORM_MONITOR,
  PLATFORM_DEVELOPER,
  PLATFORM_OPERATOR,
  PLATFORM_CICD,
] as const;
