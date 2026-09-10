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

/**
 * Every role, in the order they are worth showing: the one that can change
 * anything first, then the rest.
 */
export const ALL_ROLES = [
  PLATFORM_ADMIN,
  PLATFORM_OPERATOR,
  PLATFORM_DEVELOPER,
  PLATFORM_MONITOR,
] as const;

/** Short labels for the roles, for anywhere one is shown to a person. */
export const ROLE_LABELS: Record<string, string> = {
  [PLATFORM_ADMIN]: "Admin",
  [PLATFORM_OPERATOR]: "Operator",
  [PLATFORM_DEVELOPER]: "Developer",
  [PLATFORM_MONITOR]: "Monitor",
};
