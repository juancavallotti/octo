"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
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
 * Every decision with consequences is made on the server: the action gates in
 * app/actions/_auth.ts, the layout checks in app/auth/guard.ts, and — once the
 * orchestrator verifies tokens — the API itself. This exists so a person is not
 * offered a button that would only fail, which is a courtesy and not a control.
 * It is the same contract `useOrchestrator` has: it reports a fact about the
 * world so the UI can be honest about it.
 *
 * It is fed from the session boundary layout, which already reads the session, so
 * it costs no extra round trip.
 */

export interface RolesContextValue {
  /** The caller's roles, empty when they hold none. */
  roles: string[];
  /** Whether the caller holds `role`. */
  has: (role: string) => boolean;
  /** Shorthand for the check nearly every caller wants. */
  isAdmin: boolean;
  /**
   * False when SSO is not configured, which is how local `task dev` runs. Every
   * check passes in that mode — the same thing app/auth/guard.ts does server-side
   * — and this says so, for anywhere the UI would rather explain than pretend.
   */
  enforced: boolean;
}

const RolesContext = createContext<RolesContextValue | null>(null);

export function RolesProvider({
  roles,
  enforced,
  children,
}: {
  roles: string[];
  enforced: boolean;
  children: ReactNode;
}) {
  const value = useMemo<RolesContextValue>(() => {
    const held = new Set(roles);
    // Unenforced means unauthenticated local development, where the server-side
    // guards pass everything. Answering anything else here would hide the UI for
    // features that would in fact work.
    const has = (role: string) => !enforced || held.has(role);
    return { roles, has, isAdmin: has(PLATFORM_ADMIN), enforced };
  }, [roles, enforced]);

  return <RolesContext.Provider value={value}>{children}</RolesContext.Provider>;
}

export function useRoles(): RolesContextValue {
  const ctx = useContext(RolesContext);
  if (!ctx) {
    throw new Error("useRoles must be used within a RolesProvider");
  }
  return ctx;
}
