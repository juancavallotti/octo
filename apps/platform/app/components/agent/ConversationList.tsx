"use client";

import { useEffect, useState } from "react";
import { History, Loader2 } from "lucide-react";
import { listConversations, readConversation } from "@/app/model/conversations";
import type { ConversationRow } from "@/app/model/conversations";
import { newTurn, type Turn } from "./turns";

/**
 * The conversations already had, so one can be picked up again.
 *
 * Loaded when the list is opened rather than with the drawer: most sessions never
 * open it, and it is two requests to a pod that may not be there.
 *
 * What it lists is the agent's own record of each conversation, which is not his
 * memory — memory is compacted, so a long conversation has had its early turns
 * replaced by a summary. Only one of the two is what a person actually read.
 */
export default function ConversationList({
  onOpen,
}: {
  onOpen: (threadId: string, turns: Turn[]) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        title="Past conversations"
        aria-label="Past conversations"
        aria-expanded={open}
        className="rounded-md p-1 text-zinc-500 hover:bg-black/[0.05] hover:text-zinc-800 dark:hover:bg-white/10 dark:hover:text-zinc-100"
      >
        <History size={15} />
      </button>
      {open && <Rows onOpen={onOpen} onDone={() => setOpen(false)} />}
    </div>
  );
}

function Rows({
  onOpen,
  onDone,
}: {
  onOpen: (threadId: string, turns: Turn[]) => void;
  onDone: () => void;
}) {
  const [rows, setRows] = useState<ConversationRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [opening, setOpening] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    listConversations()
      .then((r) => live && setRows(r))
      .catch((e: Error) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, []);

  const pick = (row: ConversationRow) => {
    // One at a time. Two reads in flight resolve in whatever order the network
    // gives them, and the loser replaces the conversation the reader chose.
    if (opening) return;
    setOpening(row.id);
    readConversation(row.id)
      .then((conversation) => {
        // Every stored turn is finished, so none of them streams. Replay is the
        // text that was said, not the working-out behind it — the tool calls and
        // the reasoning belonged to the run and are gone with it.
        onOpen(
          row.id,
          conversation.turns.map((t, i) => newTurn(`${row.id}-${i}`, t.role, t.text)),
        );
        onDone();
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setOpening(null));
  };

  return (
    <div className="absolute right-0 z-10 mt-1 max-h-80 w-72 overflow-y-auto rounded-md border border-black/10 bg-white py-1 shadow-lg dark:border-white/15 dark:bg-zinc-800">
      {error && <p className="px-3 py-2 text-[11px] text-red-500">{error}</p>}
      {!rows && !error && <p className="px-3 py-2 text-[11px] text-zinc-500">Looking…</p>}
      {rows?.length === 0 && (
        <p className="px-3 py-2 text-[11px] text-zinc-500">Nothing yet — this is the first.</p>
      )}
      {rows?.map((row) => (
        <button
          key={row.id}
          type="button"
          onClick={() => pick(row)}
          className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-black/[0.04] dark:hover:bg-white/10"
        >
          <span className="min-w-0 flex-1 truncate">{row.title || "Untitled"}</span>
          {opening === row.id ? (
            <Loader2 size={11} className="shrink-0 animate-spin text-zinc-400" />
          ) : (
            <span className="shrink-0 font-mono text-[10px] tabular-nums text-zinc-400">
              {shortDate(row.updatedAt)}
            </span>
          )}
        </button>
      ))}
    </div>
  );
}

/**
 * The date, short enough for a row.
 *
 * A value that is not one shows nothing rather than "Invalid Date" — and rather
 * than the raw string, which for the RFC 3339 stamp this is fed would be
 * twenty-five characters in a column sized for six.
 */
function shortDate(value: string): string {
  const at = new Date(value);
  if (Number.isNaN(at.getTime())) return "";
  return at.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
