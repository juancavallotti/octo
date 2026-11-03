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

/** Short labels for the roles, for anywhere one is shown to a person. */
export const ROLE_LABELS: Record<string, string> = {
  [PLATFORM_ADMIN]: "Admin",
  [PLATFORM_OPERATOR]: "Operator",
  [PLATFORM_DEVELOPER]: "Developer",
  [PLATFORM_MONITOR]: "Monitor",
};

/**
 * Whether somebody holding `held` may hand `role` on to a key, mirroring
 * MayDelegate in iam/internal/user/roles.go: they hold it, or they are an admin.
 * Roles are facets, not a ladder. iam decides; this only keeps a person from being
 * offered a role it would refuse.
 */
export function mayDelegate(held: readonly string[], role: string): boolean {
  return held.includes(role) || held.includes(PLATFORM_ADMIN);
}

/** The roles an API key may carry, in presentation order: never admin. */
export const KEY_ROLES = [PLATFORM_MONITOR, PLATFORM_DEVELOPER, PLATFORM_OPERATOR] as const;
