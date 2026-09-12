"use client";

import { useEffect, useState } from "react";
import type { EditorPrefs } from "@octo/editor";
import { desktopBridge } from "@/app/desktop";

/**
 * The editor preferences the desktop shell is holding, or null when there is no shell.
 *
 * Null is the answer in a browser and in Docker, and the editor reads that as "every
 * preference at its default" — which is deliberately not the same as "off": a host that
 * grows a preferences UI later changes this hook and nothing else.
 *
 * The shell pushes changes rather than being polled, because its Settings window is open
 * *beside* this page: a checkbox ticked there should take effect here without a reload.
 */
export function useDesktopPrefs(): Partial<EditorPrefs> | null {
  const [prefs, setPrefs] = useState<Partial<EditorPrefs> | null>(null);

  useEffect(() => {
    const bridge = desktopBridge();
    // Checked at runtime, not just in the type: `desktopBridge()` is a cast over
    // whatever the preload happened to expose, so a shell older than these methods
    // reaches here typed as if it had them. Calling one then throws synchronously —
    // before the `.catch` is attached, and before the cleanup is registered — and takes
    // the page down over a preference. A shell that cannot answer keeps the defaults.
    if (typeof bridge?.prefs !== "function" || typeof bridge.onPrefsChanged !== "function") {
      return;
    }
    let live = true;
    void bridge
      .prefs()
      .then((p) => {
        if (live) setPrefs(p);
      })
      .catch(() => {});
    const stop = bridge.onPrefsChanged((p) => setPrefs(p));
    return () => {
      live = false;
      stop();
    };
  }, []);

  return prefs;
}
