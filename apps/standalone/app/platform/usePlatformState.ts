"use client";

import { useEffect, useState } from "react";
import { paasBridge } from "../desktop";
import type { PlatformState } from "./types";

/**
 * The platform as the shell last reported it, kept current by its pushes. Both the
 * Platform window and the editor's folder picker read it, so an action in one shows
 * in the other at once.
 */
export function usePlatformState(): PlatformState | null {
  const [state, setState] = useState<PlatformState | null>(null);

  useEffect(() => {
    const paas = paasBridge();
    if (!paas) return;
    let cancelled = false;
    let pushed = false;
    const off = paas.onState((s) => {
      pushed = true;
      setState(s);
    });
    paas
      .state()
      .then((s) => {
        // A push that already arrived is newer than this reply, which was read
        // before it was sent.
        if (!cancelled && !pushed) setState(s);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
      off();
    };
  }, []);

  return state;
}
