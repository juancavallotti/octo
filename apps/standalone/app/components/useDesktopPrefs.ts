"use client";

import { useEffect, useState } from "react";
import type { EditorPrefs } from "@octo/editor";
import { desktopBridge } from "@/app/desktop";

/**
 * The editor preferences a hosting shell is holding, or null when nothing is hosting
 * the page — which the editor reads as "every preference at its default", not as "off".
 * Changes are pushed rather than polled, so one made while this page is open takes
 * effect without a reload.
 */
export function useDesktopPrefs(): Partial<EditorPrefs> | null {
  const [prefs, setPrefs] = useState<Partial<EditorPrefs> | null>(null);

  useEffect(() => {
    const bridge = desktopBridge();
    // Checked at runtime, not just in the type: the bridge is a cast over whatever the
    // host exposed, and an older one reaches here typed as if it had these methods.
    // Calling a missing one throws before the `.catch` is attached.
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
