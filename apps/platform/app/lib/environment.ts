/**
 * The environment the caller has chosen, and what they hold in it. Its own
 * module, free of any server import, so pure helpers and the browser can use it
 * without pulling the actions in behind it.
 */

/** The default environment's name: the release namespace, which every install has. */
export const DEFAULT_ENVIRONMENT = "octo";

/** The cookie the chosen environment is kept in. Absent means the default. */
export const ENVIRONMENT_COOKIE = "octo-environment";

/** The header every call to the platform names the environment in. */
export const ENVIRONMENT_HEADER = "X-Octo-Environment";

const ADMIN = "platform:admin";

/** The empty string for the default environment, by either spelling. */
export function normalizeEnvironment(environment: string | undefined | null): string {
  const name = (environment ?? "").trim();
  return name === DEFAULT_ENVIRONMENT ? "" : name;
}

/**
 * The roles a person holds in `environment`: their default roles there, the
 * ones granted per environment elsewhere, and admin everywhere — platform:admin
 * is only ever granted in the default and reaches every environment. The same
 * rule the services apply to the token, so the screens offer what the
 * services will allow.
 */
export function rolesIn(
  roles: string[],
  envRoles: Record<string, string[]> | undefined,
  environment: string,
): string[] {
  const name = normalizeEnvironment(environment);
  if (name === "") return roles;
  const held = [...(envRoles?.[name] ?? [])];
  if (roles.includes(ADMIN) && !held.includes(ADMIN)) held.push(ADMIN);
  return held;
}

/** Whether a person holds anything at all in `environment`. */
export function holdsAnyIn(
  roles: string[],
  envRoles: Record<string, string[]> | undefined,
  environment: string,
): boolean {
  return rolesIn(roles, envRoles, environment).length > 0;
}
