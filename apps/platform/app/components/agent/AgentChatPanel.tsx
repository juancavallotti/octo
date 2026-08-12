"use client";

import { useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { ChevronDown, MessageSquarePlus, Send, Square } from "lucide-react";
import AgentMessage from "./AgentMessage";
import { useAgentChat } from "./useAgentChat";

/**
 * The conversation panel: transcript, composer, and the navigation the agent asks
 * for.
 *
 * `fixed` because the signed-in shell is `overflow-hidden flex flex-col`, and above
 * the account menu's stacking so the panel is not clipped by it.
 */
export default function AgentChatPanel({
  userKey,
  onCollapse,
}: {
  userKey: string;
  onCollapse: () => void;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [draft, setDraft] = useState("");
  const scroller = useRef<HTMLDivElement>(null);

  const chat = useAgentChat(userKey, pathname, (target) => {
    // The path was already checked against this app's shape when the frame was
    // parsed; router.push keeps it a client-side navigation, so the panel and the
    // conversation in it survive the move.
    router.push(target.path);
  });

  // Follow the answer as it streams. Jumping to the bottom on every token is right
  // here in a way it would not be in a long document: the newest text is the point.
  useEffect(() => {
    scroller.current?.scrollTo({ top: scroller.current.scrollHeight });
  }, [chat.turns]);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    chat.send(draft);
    setDraft("");
  };

  return (
    <div className="fixed right-4 bottom-4 z-40 flex h-[min(36rem,calc(100vh-6rem))] w-[min(26rem,calc(100vw-2rem))] flex-col rounded-xl border border-black/10 bg-white shadow-xl dark:border-white/15 dark:bg-zinc-900">
      <header className="flex items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
        <span className="text-sm font-semibold">Dr. Octo</span>
        <span className="text-xs text-zinc-500">at your service</span>
        <div className="ml-auto flex items-center gap-1">
          <button
            type="button"
            onClick={chat.reset}
            title="New conversation"
            aria-label="New conversation"
            className="rounded-md p-1 text-zinc-500 hover:bg-black/[0.05] hover:text-zinc-800 dark:hover:bg-white/10 dark:hover:text-zinc-100"
          >
            <MessageSquarePlus size={15} />
          </button>
          <button
            type="button"
            onClick={onCollapse}
            title="Collapse"
            aria-label="Collapse"
            className="rounded-md p-1 text-zinc-500 hover:bg-black/[0.05] hover:text-zinc-800 dark:hover:bg-white/10 dark:hover:text-zinc-100"
          >
            <ChevronDown size={15} />
          </button>
        </div>
      </header>

      <div ref={scroller} className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-3 py-3">
        {chat.turns.length === 0 && (
          <div className="m-auto max-w-[18rem] text-center text-xs text-zinc-500">
            <p>
              Ask about this installation — an integration, a deployment that will not
              start, or a flow you are writing.
            </p>
            <p className="mt-2">
              He knows which page you are on, and can take you to another one.
            </p>
          </div>
        )}

        {chat.turns.map((turn) => (
          <AgentMessage key={turn.id} turn={turn} />
        ))}

        {chat.error && <p className="text-xs text-red-500">{chat.error}</p>}
      </div>

      <form
        onSubmit={submit}
        className="flex items-end gap-2 border-t border-black/10 px-3 py-2 dark:border-white/10"
      >
        <textarea
          value={draft}
          rows={1}
          placeholder="Ask Dr. Octo…"
          aria-label="Message"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            // Enter sends, shift+enter breaks the line — the convention every chat
            // input follows, and the one a multi-line paste needs.
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit(e);
            }
          }}
          className="max-h-32 min-h-[2rem] flex-1 resize-none rounded-md border border-black/10 bg-transparent px-2 py-1.5 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
        />
        {chat.busy ? (
          <button
            type="button"
            onClick={chat.stop}
            title="Stop"
            aria-label="Stop"
            className="rounded-md bg-zinc-200 p-2 text-zinc-700 hover:bg-zinc-300 dark:bg-zinc-700 dark:text-zinc-100 dark:hover:bg-zinc-600"
          >
            <Square size={14} />
          </button>
        ) : (
          <button
            type="submit"
            disabled={!draft.trim()}
            title="Send"
            aria-label="Send"
            className="rounded-md bg-sky-600 p-2 text-white transition-colors hover:bg-sky-500 disabled:opacity-40"
          >
            <Send size={14} />
          </button>
        )}
      </form>
    </div>
  );
}
