import { holdsAnyIn } from "@/app/lib/environment";

/** One option the switcher offers. */
export interface EnvironmentOption {
  /** What the cookie and the header carry: "" for the default. */
  name: string;
  /** The display name, or the name when it has none. */
  label: string;
  /** The name as people type it: "octo" for the default. */
  slug: string;
  /** The Kubernetes namespace it runs in. */
  namespace: string;
  description: string;
  isDefault: boolean;
}

/**
 * The environments a person may switch to: every one for an administrator,
 * otherwise those where they hold any role. Each is labelled by its display
 * name when it has one.
 */
export function visibleEnvironments(
  all: { name: string; displayName: string; namespace: string; description: string; default: boolean }[],
  roles: string[],
  envRoles: Record<string, string[]>,
): EnvironmentOption[] {
  return all
    .filter((e) => holdsAnyIn(roles, envRoles, e.default ? "" : e.name))
    .map((e) => ({
      name: e.default ? "" : e.name,
      label: e.displayName || e.name,
      slug: e.name,
      namespace: e.namespace,
      description: e.description,
      isDefault: e.default,
    }));
}

/**
 * Where to land after switching. A page about one integration, deployment or
 * trace names something that does not exist in the other environment, so the
 * switch goes to the top of that section instead; every other page reloads as
 * it is.
 */
export function landingAfterSwitch(pathname: string): string {
  const segments = pathname.split("/").filter(Boolean);
  if (segments[0] !== "platform" || segments.length < 3) return pathname;
  const section = segments[1];
  if (section === "i") return "/platform/integrations";
  if (["integrations", "metrics", "traces", "logs"].includes(section)) return `/platform/${section}`;
  return pathname;
}
