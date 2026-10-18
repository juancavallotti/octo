"use client";

import { useConsole, useConsoleCollapsed } from "../run/console";
import { useTerminal } from "../providers/TerminalProvider";

export interface TerminalCommands {
  /** Whether the host supplied a terminal; without one both commands are no-ops. */
  available: boolean;
  /**
   * VS Code's toggle: hide the console from inside the terminal, focus the terminal
   * when it is showing but the keyboard is elsewhere, and otherwise show it.
   */
  toggle(): void;
  /** Start another shell and show it. */
  open(): void;
}

/**
 * What Ctrl+` and Ctrl+Shift+` do, and what a host's Terminal menu items do, from one
 * place, as useRunCommands is for running.
 */
export function useTerminalCommands(): TerminalCommands {
  const terminal = useTerminal();
  const { tab, openTo, setOverride } = useConsole();
  const { collapsed } = useConsoleCollapsed();

  return {
    available: terminal !== null,
    toggle() {
      if (!terminal) return;
      if (!collapsed && tab === "terminal") {
        if (terminal.focused()) setOverride(true);
        else terminal.focus();
        return;
      }
      // The tab focuses the active session as it mounts.
      openTo("terminal");
    },
    open() {
      if (!terminal) return;
      openTo("terminal");
      void terminal.open();
    },
  };
}
