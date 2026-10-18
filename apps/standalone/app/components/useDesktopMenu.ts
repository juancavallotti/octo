"use client";

import { useEffect, useRef } from "react";
import { isEditorCommand, useEditorCommands } from "@octo/editor";
import { desktopBridge } from "@/app/desktop";

/**
 * Join the hosting shell's menu bar to the editor, both ways: a menu item names a
 * command and the editor carries it out, and the editor reports what it can do so the
 * menu greys out, checks and labels its items to match. Nothing happens in a plain
 * browser, which has no menu bar to join.
 */
export function useDesktopMenu(): void {
  const { run, status } = useEditorCommands();

  useEffect(() => {
    const bridge = desktopBridge();
    // Checked at runtime: an older shell reaches here typed as if it had this method.
    if (typeof bridge?.onMenuCommand !== "function") return;
    return bridge.onMenuCommand((command) => {
      if (isEditorCommand(command)) run(command);
    });
  }, [run]);

  // Reported only when it changes: the shell rebuilds its whole menu on each report.
  const reported = useRef<string | null>(null);
  const serialized = JSON.stringify(status);
  useEffect(() => {
    const bridge = desktopBridge();
    if (typeof bridge?.reportMenuStatus !== "function") return;
    if (serialized === reported.current) return;
    reported.current = serialized;
    void bridge.reportMenuStatus(JSON.parse(serialized)).catch(() => {});
  }, [serialized]);

  // Gone, the page has nothing to report; the shell's menu goes back to its defaults.
  // What was last reported is forgotten with it, so a remount reports afresh.
  useEffect(() => {
    return () => {
      reported.current = null;
      void desktopBridge()?.reportMenuStatus?.(null).catch(() => {});
    };
  }, []);
}
