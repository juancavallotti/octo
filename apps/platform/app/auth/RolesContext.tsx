"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
import { capabilitiesOf, type Capabilities } from "./capabilities";
import { PLATFORM_ADMIN } from "./roles";

/**
 * The signed-in caller's roles, for client components that want to know what to
 * render.
 *
 * ## This decides what to show, never what to permit
 *
 * Everything here came out of a cookie the browser is holding. It is stale by up
 * to one token lifetime after a role is revoked, and a patched bundle can hand it
 * whatever it likes. Nothing that has an effect may be gated on it.
 *
 * Every decision with consequences is made on the server. This exists so a person
 * is not offered a button that would only fail, which is a courtesy and not a
 * control.
 */

export interface RolesContextValue {
  /** The caller's roles, empty when they hold none. */
  roles: string[];
  /** Whether the caller holds `role`. */
  has: (role: string) => boolean;
  /** Shorthand for the check nearly every caller wants. */
  isAdmin: boolean;
  /** What to offer this caller, in the verbs the screens are written in. */
  can: Capabilities;
}

const RolesContext = createContext<RolesContextValue | null>(null);

export function RolesProvider({
  roles,
  mayWrite,
  children,
}: {
  roles: string[];
  /**
   * Whether this installation lets this caller write at all. Decided on the
   * server, because the setting behind it is server-only, and passed in rather
   * than derived here for the same reason.
   */
  mayWrite: boolean;
  children: ReactNode;
}) {
  const value = useMemo<RolesContextValue>(() => {
    const held = new Set(roles);
    const has = (role: string) => held.has(role);
    return {
      roles,
      has,
      isAdmin: has(PLATFORM_ADMIN),
      can: capabilitiesOf(roles, mayWrite),
    };
  }, [roles, mayWrite]);

  return <RolesContext.Provider value={value}>{children}</RolesContext.Provider>;
}

export function useRoles(): RolesContextValue {
  const ctx = useContext(RolesContext);
  if (!ctx) {
    throw new Error("useRoles must be used within a RolesProvider");
  }
  return ctx;
}
