"use client";

import { useEffect, useRef } from "react";
import { useTerminalCommands, type TerminalCommands } from "../commands/useTerminalCommands";
import { isTerminalShortcut } from "../terminal/screen";

/**
 * Ctrl+` toggles the terminal; Ctrl+Shift+` starts another. VS Code's keys, and Ctrl
 * on every platform as there: Cmd+` is the Mac's switch-window key.
 *
 * Not guarded on the focused element: the terminal's own input is a text area, and
 * the key that hides the terminal has to work from inside it.
 */
export function useTerminalShortcuts(): void {
  const commands = useTerminalCommands();
  // Read through a ref so the listener registers once, as useRunShortcuts' does.
  const latest = useRef<TerminalCommands>(commands);
  latest.current = commands;

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!isTerminalShortcut(e) || !latest.current.available) return;
      e.preventDefault();
      if (e.shiftKey) latest.current.open();
      else latest.current.toggle();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);
}
