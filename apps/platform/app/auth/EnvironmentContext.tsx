"use client";

import { createContext, useContext, type ReactNode } from "react";

/**
 * The environment the caller is working in, and what they hold in each, for
 * the switcher. Like the roles context it decides what to show and nothing
 * else: every call is judged by the services against the token.
 */
export interface EnvironmentContextValue {
  /** The chosen environment, "" for the default. */
  environment: string;
  /** Roles in the default environment. */
  roles: string[];
  /** Roles in every other environment, by name. */
  envRoles: Record<string, string[]>;
}

const EnvironmentContext = createContext<EnvironmentContextValue | null>(null);

export function EnvironmentProvider({
  value,
  children,
}: {
  value: EnvironmentContextValue;
  children: ReactNode;
}) {
  return <EnvironmentContext.Provider value={value}>{children}</EnvironmentContext.Provider>;
}

/** The caller's environment, or null outside a signed-in page. */
export function useEnvironment(): EnvironmentContextValue | null {
  return useContext(EnvironmentContext);
}
