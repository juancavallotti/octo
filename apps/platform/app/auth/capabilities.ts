/**
 * What the signed-in caller may do, in the vocabulary the screens are built in.
 *
 * Three verbs rather than five roles. A screen asks "may this person deploy",
 * not "does this person hold platform:operator or platform:admin, and is
 * platform:operator among the roles this installation lets write" — and asking it
 * the second way means every screen has to be edited when the answer changes.
 *
 * `mayWrite` is the installation's own answer to whether this caller writes at
 * all, which narrows all three: a role is necessary and not sufficient.
 */

import {
  PLATFORM_ADMIN,
  PLATFORM_DEVELOPER,
  PLATFORM_OPERATOR,
} from "./roles";

export interface Capabilities {
  /** Create and change integrations, their resources, and dev runs. */
  build: boolean;
  /** Deploy, roll out, scale and remove deployments. */
  deploy: boolean;
  /** The installation's own settings, secrets, people and agent. */
  administer: boolean;
}

/**
 * Why a control is unavailable, for the `title` on the control itself.
 *
 * The first two name both causes, because a role is necessary and not
 * sufficient: an installation that has narrowed who writes takes them away from
 * somebody who does hold the role, and telling that person they need a role they
 * already have would send them looking in the wrong place.
 */
export const CAPABILITY_REASONS: Record<keyof Capabilities, string> = {
  build: "Needs the Developer or Operator role, and write access on this installation",
  deploy: "Needs the Operator role, and write access on this installation",
  administer: "Needs the Admin role",
};

/** Derive the three from the caller's roles and whether they may write at all. */
export function capabilitiesOf(
  roles: readonly string[],
  mayWrite: boolean,
): Capabilities {
  const held = new Set(roles);
  const admin = held.has(PLATFORM_ADMIN);
  return {
    build:
      mayWrite &&
      (admin || held.has(PLATFORM_OPERATOR) || held.has(PLATFORM_DEVELOPER)),
    deploy: mayWrite && (admin || held.has(PLATFORM_OPERATOR)),
    administer: admin,
  };
}
