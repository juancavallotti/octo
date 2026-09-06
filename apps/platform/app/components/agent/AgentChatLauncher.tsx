"use client";

import { useCallback, useEffect, useState } from "react";
import dynamic from "next/dynamic";
import { Bot } from "lucide-react";
import {
  clampWidth,
  DEFAULT_WIDTH,
  readDocked,
  readWidth,
  writeDocked,
  writeWidth,
} from "./panelPrefs";

/**
 * Loaded on demand, because this launcher sits in the layout every signed-in page
 * shares and the drawer brings a Markdown renderer with it. Installing the agent is
 * a deliberate act, so on most installations that is weight on every page for
 * something nobody can open — and even where he is installed, it is weight before
 * anybody asks him anything.
 */
const AgentDrawer = dynamic(() => import("./AgentDrawer"), { ssr: false });

/**
 * The shell every signed-in page sits in, and the button that opens the chat
 * beside it.
 *
 * It probes once on mount and renders only the page when the agent is not deployed
 * — which is most installations, since installing him is a deliberate act. A
 * launcher that opened onto an error would be worse than no launcher.
 *
 * It owns the page's layout rather than only floating over it because the panel can
 * be pinned: docked, the page shrinks into the space beside the panel; floating, it
 * overlays as before. The page is wrapped either way so that pinning is a class
 * change rather than a different tree.
 *
 * The drawer mounts on first open and then stays mounted, hidden when collapsed —
 * so minimising it keeps the conversation and lets an answer in flight finish
 * arriving. Before that first open nothing of it exists, which is what keeps an
 * installation that never uses the agent from paying for it.
 */
export default function AgentChatLauncher({
  userKey,
  children,
}: {
  userKey: string;
  children: React.ReactNode;
}) {
  const [available, setAvailable] = useState(false);
  const [open, setOpen] = useState(false);
  // Whether the panel has ever been opened. Separate from `open` because the panel
  // stays mounted once it exists, and this is what keeps it from existing at all
  // until somebody asks for it.
  const [opened, setOpened] = useState(false);
  // Layout preferences, hydrated after mount — see the effect below.
  const [docked, setDocked] = useState(false);
  const [width, setWidth] = useState(DEFAULT_WIDTH);
  // Lifted out of the drawer so the collapsed button can say he is still working.
  const [busy, setBusy] = useState(false);

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

  useEffect(() => {
    // Reading storage during render would mismatch the server-rendered markup —
    // this layout is force-dynamic, so the shell really is rendered on the server —
    // so the remembered layout has to arrive after hydration. The panel is closed
    // at that point, so nothing visibly moves.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setDocked(readDocked());
    setWidth(readWidth());
  }, []);

  const toggleDock = useCallback(() => {
    setDocked((prev) => {
      writeDocked(!prev);
      return !prev;
    });
  }, []);

  const resize = useCallback((next: number) => setWidth(clampWidth(next)), []);
  // Written once the drag ends rather than on every move: the panel follows the
  // pointer through React state, and storage does not need sixty writes a second
  // to end up at the same number.
  const commitWidth = useCallback((next: number) => writeWidth(next), []);

  return (
    <div className="flex h-full min-h-0 flex-1">
      <div className="flex h-full min-w-0 flex-1 flex-col">{children}</div>

      {/* One slot for the panel, three appearances: hidden, docked beside the page,
          or floating over it. It is never re-parented, because moving it in the tree
          would remount the drawer — aborting the fetch that is streaming an answer
          and losing the conversation with it. Which is also why collapsing hides it
          rather than unmounting it: reopen and the answer is there, finished.

          Nothing is mounted before the first open, so an installation where the
          agent is never used pays for none of it. */}
      {opened && (
        <div
          style={open ? { width, maxWidth: "100vw" } : undefined}
          className={
            !open
              ? "hidden"
              : docked
                ? "shrink-0"
                : "fixed inset-y-0 right-0 z-40"
          }
        >
          <AgentDrawer
            userKey={userKey}
            onCollapse={() => setOpen(false)}
            docked={docked}
            onToggleDock={toggleDock}
            width={width}
            onResize={resize}
            onResizeEnd={commitWidth}
            onBusy={setBusy}
          />
        </div>
      )}

      {available && !open && (
        <div className="fixed right-4 bottom-4 z-40">
          {/* Behind the button, and only while there is something to report. */}
          {busy && <span aria-hidden className="agent-halo" />}
          <button
            type="button"
            onClick={() => {
              setOpened(true);
              setOpen(true);
            }}
            title={busy ? "Dr. Octo is working…" : "Ask Dr. Octo"}
            aria-label={busy ? "Dr. Octo is working…" : "Ask Dr. Octo"}
            className="relative flex h-11 w-11 items-center justify-center rounded-full bg-sky-600 text-white shadow-lg transition-colors hover:bg-sky-500"
          >
            <Bot size={20} />
          </button>
        </div>
      )}
    </div>
  );
}
