/**
 * The environment the caller has chosen, and what they hold in it. Its own
 * module, free of any server import, so pure helpers and the browser can use it
 * without pulling the actions in behind it.
 */

/** The default environment's name: the release namespace, which every install has. */
export const DEFAULT_ENVIRONMENT = "octo";

/** The cookie the chosen environment is kept in. Absent means the default. */
export const ENVIRONMENT_COOKIE = "octo-environment";

/** A year: the choice is a preference, kept until it is changed. */
export const ENVIRONMENT_COOKIE_MAX_AGE = 365 * 24 * 60 * 60;

/**
 * The query parameter a link names an environment in: any platform page with
 * `?env=<name>` opens there, so a bookmark, a shared link or the agent can say
 * which environment it means. The cookie stays where the choice is kept.
 */
export const ENVIRONMENT_PARAM = "env";

/**
 * What a request's `?env=` asks of the selection: the environment to choose ("" for
 * the default), or null when there is nothing to change — no parameter, the one
 * already chosen, or one the person holds no role in. Pure, so the request proxy
 * and its tests share it.
 */
export function selectionFromLink(
  param: string | null,
  chosen: string | undefined,
  roles: string[],
  envRoles: Record<string, string[]> | undefined,
): string | null {
  if (param === null) return null;
  const wanted = normalizeEnvironment(param);
  if (wanted === normalizeEnvironment(chosen)) return null;
  if (wanted !== "" && rolesIn(roles, envRoles, wanted).length === 0) return null;
  return wanted;
}

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
  const held = [...heldIn(envRoles, name)];
  if (roles.includes(ADMIN) && !held.includes(ADMIN)) held.push(ADMIN);
  return held;
}

/**
 * The roles granted in one environment, read as the map's own entry only. An
 * environment may be named "constructor" — a valid DNS label — and the selection
 * cookie is the caller's to set, so a lookup through the prototype would hand
 * back a function where a list of roles belongs.
 */
export function heldIn(envRoles: Record<string, string[]> | undefined, environment: string): string[] {
  return envRoles && Object.hasOwn(envRoles, environment) ? envRoles[environment] : [];
}

/** Whether a person holds anything at all in `environment`. */
export function holdsAnyIn(
  roles: string[],
  envRoles: Record<string, string[]> | undefined,
  environment: string,
): boolean {
  return rolesIn(roles, envRoles, environment).length > 0;
}

/**
 * Whether opening `path` (an address on this site) moves the page from `current`
 * into another environment: true when it names one with `?env=` that is not
 * `current`. An address that names none stays where the page is.
 */
export function leavesEnvironment(path: string, current: string): boolean {
  const named = new URL(path, "http://site").searchParams.get(ENVIRONMENT_PARAM);
  return named !== null && normalizeEnvironment(named) !== normalizeEnvironment(current);
}
