import { rolesIn } from "@/app/lib/environment";

const BUILDERS = ["platform:admin", "platform:operator", "platform:developer"];

/** One environment a version can be promoted into. */
export interface PromoteTarget {
  /** The name the orchestrator takes ("octo" for the default). */
  name: string;
  label: string;
  namespace: string;
}

/**
 * Where a person may promote a version from `current`: every other environment
 * in which they hold a role that builds — the same rule the orchestrator applies
 * to the write, so the menu offers what it will accept.
 */
export function promoteTargets(
  all: { name: string; displayName: string; namespace: string; default: boolean }[],
  current: string,
  roles: string[],
  envRoles: Record<string, string[]>,
): PromoteTarget[] {
  return all
    .filter((e) => (e.default ? "" : e.name) !== current)
    .filter((e) => rolesIn(roles, envRoles, e.default ? "" : e.name).some((r) => BUILDERS.includes(r)))
    .map((e) => ({ name: e.name, label: e.displayName || e.name, namespace: e.namespace }));
}
