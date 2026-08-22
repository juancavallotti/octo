"use client";

import { useEffect, useState } from "react";
import dynamic from "next/dynamic";
import { Bot } from "lucide-react";

/**
 * Loaded on demand, because this launcher sits in the layout every signed-in page
 * shares and the drawer brings a Markdown renderer with it. Installing the agent is
 * a deliberate act, so on most installations that is weight on every page for
 * something nobody can open — and even where he is installed, it is weight before
 * anybody asks him anything.
 */
const AgentDrawer = dynamic(() => import("./AgentDrawer"), { ssr: false });

/**
 * The button that opens the chat, mounted in the signed-in shell so it is on every
 * page.
 *
 * It probes once on mount and renders nothing when the agent is not deployed —
 * which is most installations, since installing him is a deliberate act. A launcher
 * that opened onto an error would be worse than no launcher.
 *
 * The drawer mounts on first open and then stays mounted, hidden when collapsed —
 * so minimising it keeps the conversation and lets an answer in flight finish
 * arriving. Before that first open nothing of it exists, which is what keeps an
 * installation that never uses the agent from paying for it.
 */
export default function AgentChatLauncher({ userKey }: { userKey: string }) {
  const [available, setAvailable] = useState(false);
  const [open, setOpen] = useState(false);
  // Whether the panel has ever been opened. Separate from `open` because the panel
  // stays mounted once it exists, and this is what keeps it from existing at all
  // until somebody asks for it.
  const [opened, setOpened] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/agent/status", { signal: controller.signal })
      .then((res) => (res.ok ? res.json() : { available: false }))
      .then((body: { available?: boolean }) => setAvailable(Boolean(body.available)))
      .catch(() => {
        // An unreachable probe means no chat, which is what the initial state
        // already says. Nothing to report on a page the user came to for something
        // else entirely.
      });
    return () => controller.abort();
  }, []);

  if (!available) return null;

  return (
    <>
      {/* Mounted from the first open onwards and hidden rather than unmounted, so
          collapsing the panel keeps the conversation and lets an answer in flight
          carry on arriving — reopen and it is there, finished. Unmounting would
          abort the fetch, which is the right thing for a tab that has gone away
          and the wrong thing for a panel somebody minimised for a minute.

          Nothing is mounted before the first open, so an installation where the
          agent is never used pays for none of it. */}
      {opened && (
        <div className={open ? undefined : "hidden"}>
          <AgentDrawer userKey={userKey} onCollapse={() => setOpen(false)} />
        </div>
      )}

      {!open && (
        <button
          type="button"
          onClick={() => {
            setOpened(true);
            setOpen(true);
          }}
          title="Ask Dr. Octo"
          aria-label="Ask Dr. Octo"
          className="fixed right-4 bottom-4 z-40 flex h-11 w-11 items-center justify-center rounded-full bg-sky-600 text-white shadow-lg transition-colors hover:bg-sky-500"
        >
          <Bot size={20} />
        </button>
      )}
    </>
  );
}
