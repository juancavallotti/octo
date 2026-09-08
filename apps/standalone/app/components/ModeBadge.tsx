"use client";

import { useSyncExternalStore } from "react";
import { isDesktop } from "../desktop";

/**
 * How this editor is being run: "desktop" inside the Electron shell, "standalone"
 * in a browser (`task dev`, the Docker image).
 *
 * Worth distinguishing rather than always saying "standalone", because the two
 * differ in ways a user reporting a problem will be asked about — where flows
 * live, whether there is a folder picker, whether an MCP endpoint is being
 * advertised.
 *
 * Read through useSyncExternalStore, which is the tool for exactly this: a value
 * the server cannot know (a global the shell's preload sets) that must not make
 * the client's first pass disagree with the server's HTML. The server snapshot is
 * "not desktop", so hydration matches and React swaps in the real answer
 * immediately afterwards — no mismatch, and no setState-in-an-effect.
 *
 * The store never changes during a session: a page either has a shell behind it
 * or it does not. Hence the no-op subscribe.
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
