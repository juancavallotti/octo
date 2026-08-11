"use client";

import { useEffect, useState } from "react";
import { Bot } from "lucide-react";
import AgentChatPanel from "./AgentChatPanel";

/**
 * The button that opens the chat, mounted in the signed-in shell so it is on every
 * page.
 *
 * It probes once on mount and renders nothing when the agent is not deployed —
 * which is most installations, since installing him is a deliberate act. A launcher
 * that opened onto an error would be worse than no launcher.
 *
 * The panel is mounted only while open, so a closed panel holds no conversation
 * state and no connection. That is deliberate rather than incidental: keeping it
 * mounted and hidden would leave a stream running behind a panel nobody can see.
 */
export default function AgentChatLauncher({ userKey }: { userKey: string }) {
  const [available, setAvailable] = useState(false);
  const [open, setOpen] = useState(false);

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

  if (open) return <AgentChatPanel userKey={userKey} onClose={() => setOpen(false)} />;

  return (
    <button
      type="button"
      onClick={() => setOpen(true)}
      title="Ask Dr. Octo"
      aria-label="Ask Dr. Octo"
      className="fixed right-4 bottom-4 z-40 flex h-11 w-11 items-center justify-center rounded-full bg-sky-600 text-white shadow-lg transition-colors hover:bg-sky-500"
    >
      <Bot size={20} />
    </button>
  );
}
