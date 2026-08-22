"use client";

import { useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { ArrowDown, MessageSquarePlus, X } from "lucide-react";
import AgentMessage from "./AgentMessage";
import ContextGauge from "./ContextGauge";
import Composer from "./Composer";
import WorkingStatus from "./WorkingStatus";
import { useAgentChat } from "./useAgentChat";
import { useStickToBottom } from "./useStickToBottom";

/**
 * The conversation drawer: transcript, composer, and the navigation the agent asks
 * for.
 *
 * Full height down the right-hand side rather than a box in the corner. The old
 * panel was 26rem by 36rem, which is two or three tool chips and a paragraph — and
 * a run that calls a few tools and explains itself does not fit in that, so
 * everything interesting was permanently scrolled past.
 *
 * It overlays rather than pushing the page, and has no backdrop, because he
 * navigates: asking "take me to that deployment" and watching the page behind the
 * drawer change is the whole point, and a modal would break it.
 *
 * `fixed` because the signed-in shell is `overflow-hidden flex flex-col`, and above
 * the account menu's stacking so the drawer is not clipped by it.
 */
export default function AgentDrawer({
  userKey,
  onCollapse,
}: {
  userKey: string;
  onCollapse: () => void;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [draft, setDraft] = useState("");

  const chat = useAgentChat(userKey, pathname, (target) => {
    // The path was already checked against this app's shape when the frame was
    // parsed; router.push keeps it a client-side navigation, so the drawer and the
    // conversation in it survive the move.
    router.push(target.path);
  });

  const { ref: scroller, following, toBottom } = useStickToBottom(chat.turns);
  const open = chat.turns.at(-1);

  const submit = () => {
    chat.send(draft);
    setDraft("");
  };

  return (
    <aside
      aria-label="Dr. Octo"
      className="fixed inset-y-0 right-0 z-40 flex w-[min(32rem,100vw)] flex-col border-l border-black/10 bg-white shadow-2xl dark:border-white/15 dark:bg-zinc-900"
    >
      <header className="flex items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
        <span className="text-sm font-semibold">Dr. Octo</span>
        <span className="text-xs text-zinc-500">at your service</span>
        <div className="ml-auto flex items-center gap-2">
          {open?.context && <ContextGauge gauge={open.context} />}
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
            title="Close"
            aria-label="Close"
            className="rounded-md p-1 text-zinc-500 hover:bg-black/[0.05] hover:text-zinc-800 dark:hover:bg-white/10 dark:hover:text-zinc-100"
          >
            <X size={15} />
          </button>
        </div>
      </header>

      <div className="relative flex min-h-0 flex-1 flex-col">
        <div
          ref={scroller}
          className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-3 py-3"
        >
          {chat.turns.length === 0 && <Empty />}

          {chat.turns.map((turn) => (
            <AgentMessage key={turn.id} turn={turn} />
          ))}

          {chat.error && <p className="text-xs text-red-500">{chat.error}</p>}
        </div>

        {/* Offered only when following is off, so it is a way back rather than a
            permanent control — and it is the only sign that scrolling away turned
            anything off. */}
        {!following && (
          <button
            type="button"
            onClick={toBottom}
            aria-label="Jump to the latest"
            className="absolute right-4 bottom-2 flex items-center gap-1 rounded-full border border-black/10 bg-white px-2 py-1 text-[11px] text-zinc-600 shadow-md dark:border-white/15 dark:bg-zinc-800 dark:text-zinc-300"
          >
            <ArrowDown size={12} />
            Latest
          </button>
        )}
      </div>

      <WorkingStatus turn={open} />
      <Composer
        draft={draft}
        onDraft={setDraft}
        onSubmit={submit}
        busy={chat.busy}
        onStop={chat.stop}
      />
    </aside>
  );
}

function Empty() {
  return (
    <div className="m-auto max-w-[22rem] text-center text-xs text-zinc-500">
      <p>
        Ask about this installation — an integration, a deployment that will not
        start, or a flow you are writing.
      </p>
      <p className="mt-2">He knows which page you are on, and can take you to another one.</p>
    </div>
  );
}
