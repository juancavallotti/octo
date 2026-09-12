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
    if (!bridge) return;
    let live = true;
    // A shell too old to answer leaves the defaults in place rather than failing the
    // page: preferences are a convenience, and the editor works without them.
    void bridge.prefs().then((p) => {
      if (live) setPrefs(p);
    }).catch(() => {});
    const stop = bridge.onPrefsChanged((p) => setPrefs(p));
    return () => {
      live = false;
      stop();
    };
  }, []);

  return prefs;
}
