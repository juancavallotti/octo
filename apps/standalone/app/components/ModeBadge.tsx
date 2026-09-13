"use client";

import { useSyncExternalStore } from "react";
import { isDesktop } from "../desktop";

/**
 * How this editor is being run: "desktop" when a shell is hosting the page,
 * "standalone" in a plain browser.
 *
 * Read through useSyncExternalStore because the value is one the server cannot know:
 * the server snapshot is "not desktop", so hydration matches and React swaps in the
 * real answer immediately afterwards. It never changes during a session, hence the
 * no-op subscribe.
 */
const subscribe = () => () => {};
export default function ModeBadge() {
  const desktop = useSyncExternalStore(subscribe, isDesktop, () => false);

  return (
    <span className="rounded bg-black/[0.06] px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-zinc-500 dark:bg-white/10">
      {desktop ? "desktop" : "standalone"}
    </span>
  );
}
