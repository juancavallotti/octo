"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { History, Loader2, Trash2 } from "lucide-react";
import {
  deleteConversation,
  listConversations,
  readConversation,
} from "@/app/model/conversations";
import type { ConversationRow } from "@/app/model/conversations";
import { AppPickerPanel } from "@/app/components/AppPickerPanel";
import { usePickerSearch } from "@/app/components/usePickerSearch";
import { newTurn, type Turn } from "./turns";

/**
 * The conversations already had, so one can be picked up again — or thrown away.
 *
 * Loaded when the list is opened rather than with the drawer: most sessions never
 * open it, and it is two requests to a pod that may not be there.
 *
 * It wears the platform's picker panel rather than a list of its own, because the
 * question it asks is the picker's question: somebody knows the name of the
 * conversation they want and not its position in a list that only grows. The panel
 * hangs from the right, since the control that opens it is an icon at the right of
 * the drawer's header.
 *
 * What it lists is the agent's own record of each conversation, which is not his
 * memory — memory is compacted, so a long conversation has had its early turns
 * replaced by a summary. Only one of the two is what a person actually read.
 */
export default function ConversationList({
  onOpen,
}: {
  onOpen: (threadId: string, turns: Turn[], title: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  const close = () => {
    setOpen(false);
    trigger.current?.focus();
  };

  return (
    <div ref={root} className="relative">
      <button
        ref={trigger}
        type="button"
        onClick={() => setOpen((v) => !v)}
        title="Past conversations"
        aria-label="Past conversations"
        aria-expanded={open}
        className="rounded-md p-1 text-zinc-500 hover:bg-black/[0.05] hover:text-zinc-800 dark:hover:bg-white/10 dark:hover:text-zinc-100"
      >
        <History size={15} />
      </button>
      {open && <Rows onOpen={onOpen} onDone={close} />}
    </div>
  );
}

function Rows({
  onOpen,
  onDone,
}: {
  onOpen: (threadId: string, turns: Turn[], title: string) => void;
  onDone: () => void;
}) {
  const [rows, setRows] = useState<ConversationRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [opening, setOpening] = useState<string | null>(null);
  /** The row whose trash has been clicked once, waiting for the confirm. */
  const [confirming, setConfirming] = useState<string | null>(null);
  const id = useId();

  useEffect(() => {
    let live = true;
    listConversations()
      .then((r) => live && setRows(r))
      .catch((e: Error) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, []);

  const pick = useCallback(
    (row: ConversationRow) => {
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
            // The stored name, falling back to the listing's: the same string, and
            // the listing has it even when the read comes back without one.
            conversation.title || row.title,
          );
          onDone();
        })
        .catch((e: Error) => setError(e.message))
        .finally(() => setOpening(null));
    },
    [opening, onOpen, onDone],
  );

  const search = usePickerSearch({
    items: rows ?? [],
    toText: (row: ConversationRow) => row.title,
    onChoose: pick,
    onClose: onDone,
  });

  /**
   * Erase a conversation, dropping it from the list rather than reloading: the
   * listing is a request to a pod that may not answer twice, and the row that just
   * went is the one thing the panel already knows the answer about. A failure puts
   * it back, since a list that quietly loses a row it could not delete is worse
   * than the error.
   */
  const erase = (row: ConversationRow) => {
    setConfirming(null);
    setRows((current) => current?.filter((r) => r.id !== row.id) ?? null);
    deleteConversation(row.id).catch((e: Error) => {
      setError(e.message);
      setRows((current) =>
        current ? [...current, row].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)) : current,
      );
    });
  };

  /**
   * Delete from the keyboard, since Tab leaves the panel and the trash would
   * otherwise be reachable by pointer alone. Same two steps as the pointer path:
   * once to arm the active row, again to erase it.
   */
  const onKeyDown = (e: React.KeyboardEvent) => {
    const row = search.matches[search.active];
    if (e.key === "Delete" && row) {
      e.preventDefault();
      if (confirming === row.id) erase(row);
      else setConfirming(row.id);
      return;
    }
    search.onKeyDown(e);
  };

  // Only a listing that never arrived replaces the panel. An error raised after
  // it loaded — a read that failed, a delete that was refused — is shown with the
  // rows still there, or the row `erase` puts back would come back invisible.
  if (!rows) {
    return (
      <div className="absolute right-0 top-full z-40 mt-1 w-72 rounded-md border border-black/10 bg-white py-1 shadow-lg dark:border-white/15 dark:bg-zinc-800">
        {error ? (
          <p className="px-3 py-2 text-[11px] text-red-500">{error}</p>
        ) : (
          <p className="px-3 py-2 text-[11px] text-zinc-500">Looking…</p>
        )}
      </div>
    );
  }

  return (
    <AppPickerPanel
      align="right"
      notice={error}
      matches={search.matches}
      sourceCount={rows.length}
      active={search.active}
      onActivate={search.setCursor}
      onChoose={pick}
      selectedKey={null}
      toKey={(row) => row.id}
      query={search.query}
      onQueryChange={search.setQuery}
      onKeyDown={onKeyDown}
      idPrefix={id}
      label="Past conversations"
      placeholder="Search conversations"
      empty="Nothing yet — this is the first."
      renderRow={(row) => (
        <span className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-xs">{row.title || "Untitled"}</span>
          {opening === row.id ? (
            <Loader2 size={11} className="shrink-0 animate-spin text-zinc-400" />
          ) : (
            <span className="shrink-0 font-mono text-[10px] tabular-nums text-zinc-400">
              {shortDate(row.updatedAt)}
            </span>
          )}
          {confirming === row.id ? (
            // The confirm is the row itself rather than a dialog: a modal over a
            // panel that closes on any outside pointer down is two things fighting
            // over the next click.
            <button
              type="button"
              aria-label={`Confirm deleting ${row.title || "Untitled"}`}
              onClick={(e) => {
                e.stopPropagation();
                erase(row);
              }}
              className="shrink-0 rounded bg-red-500/10 px-1 text-[10px] font-medium text-red-500"
            >
              Delete?
            </button>
          ) : (
            <button
              type="button"
              aria-label={`Delete ${row.title || "Untitled"}`}
              onClick={(e) => {
                e.stopPropagation();
                setConfirming(row.id);
              }}
              className="shrink-0 rounded p-0.5 text-zinc-400 hover:bg-red-500/10 hover:text-red-500"
            >
              <Trash2 size={12} />
            </button>
          )}
        </span>
      )}
    />
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
