"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";

/**
 * Who owns the bottom console's tab and collapsed state.
 *
 * LogPanel used to hold both in local state, which was fine while the only thing that
 * opened the panel was a run starting. Now a flow run has to be able to say "show the
 * results" (or "show the problems"), and it happens somewhere else entirely — so the
 * state moves up here, where both can reach it.
 *
 * The collapse rule is deliberately kept as it was: `collapsed` is *derived* from
 * whether a runner is live, until the user overrides it with the toggle. So pressing
 * Run opens the panel without anyone writing state, and once the user has an opinion,
 * it sticks.
 */

export type ConsoleTab = "problems" | "logs" | "results" | "env";

interface ConsoleValue {
  tab: ConsoleTab;
  setTab(tab: ConsoleTab): void;
  /** The user's explicit collapse choice, or null while it is still derived. */
  override: boolean | null;
  setOverride(collapsed: boolean | null): void;
  /** Show a tab and make sure the panel is open — what a run calls to surface itself. */
  openTo(tab: ConsoleTab): void;
}

const ConsoleContext = createContext<ConsoleValue | null>(null);

export function ConsoleProvider({ children }: { children: ReactNode }) {
  const [tab, setTab] = useState<ConsoleTab>("logs");
  const [override, setOverride] = useState<boolean | null>(null);

  const openTo = useCallback((next: ConsoleTab) => {
    setTab(next);
    setOverride(false); // expand, whatever the user last chose: they asked for this
  }, []);

  const value = useMemo<ConsoleValue>(
    () => ({ tab, setTab, override, setOverride, openTo }),
    [tab, override, openTo],
  );

  return <ConsoleContext.Provider value={value}>{children}</ConsoleContext.Provider>;
}

/**
 * The console's tab state. Unlike the optional capability contexts, this is always
 * mounted by EditorRoot — a missing provider is a wiring bug, not a supported mode, so
 * it throws rather than silently rendering a console nothing can drive.
 */
export function useConsole(): ConsoleValue {
  const value = useContext(ConsoleContext);
  if (!value) throw new Error("useConsole must be used within a ConsoleProvider");
  return value;
}
