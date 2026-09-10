"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Check, FolderOpen, Search } from "lucide-react";

/**
 * "What am I working on, and what else could I be working on" — the chip at the
 * right of the header, and the menu it opens.
 *
 * One component, two meanings. In the desktop shell the items are folders and
 * picking one switches the folder being served; in the platform they are
 * integrations and picking one opens it. The shapes are identical — a current
 * thing, a list of others, pick to switch — so the difference is what a host
 * passes in, not a second component to keep in step with this one.
 *
 * `action` is the row above the list (the shell's "Open folder…"). A host that has
 * no such thing — the platform cannot open a folder — passes nothing and the row
 * is not there.
 */

export interface PickerItem {
  /** Stable key, and what `onSelect` is called with. */
  id: string;
  name: string;
  /** Optional hover text, e.g. a folder's absolute path. */
  hint?: string;
}

export default function WorkspacePicker({
  label,
  current,
  currentHint,
  items,
  onSelect,
  onOpen,
  action,
  searchPlaceholder = "Search…",
  emptyLabel = "Nothing else here",
}: {
  /** Accessible name for the chip, e.g. "Project folder" / "Integration". */
  label: string;
  /** The thing being worked on now. */
  current: string;
  currentHint?: string;
  items: PickerItem[];
  onSelect(id: string): void;
  /** Called when the menu opens, for a host that refreshes its list then. */
  onOpen?(): void;
  action?: { label: string; icon?: React.ReactNode; onClick(): void };
  searchPlaceholder?: string;
  emptyLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    onOpen?.();
    // Fresh search each time: a filter left over from the last visit hides things
    // the user has no reason to think are hidden.
    setQuery("");
    // onOpen is a host callback and would otherwise re-run this on every render of
    // a host that builds it inline.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter((i) => i.name.toLowerCase().includes(q));
  }, [items, query]);

  const row =
    "flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]";

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        title={currentHint || undefined}
        aria-label={label}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm text-zinc-600 transition-colors hover:border-black/10 hover:text-zinc-900 dark:text-zinc-300 dark:hover:border-white/15 dark:hover:text-zinc-100"
      >
        <FolderOpen size={14} className="shrink-0 text-zinc-400" />
        <span className="max-w-[10rem] truncate">{current}</span>
      </button>

      {open && (
        /* Anchored to the right edge: the chip sits near the end of the bar, so a
           left-anchored menu would hang off it. */
        <div className="absolute right-0 top-full z-50 mt-2 w-72 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          {action && (
            <button
              type="button"
              onClick={() => {
                setOpen(false);
                action.onClick();
              }}
              className={`${row} border-b border-black/5 dark:border-white/5`}
            >
              {action.icon}
              <span className="flex-1">{action.label}</span>
            </button>
          )}

          <div className="flex items-center gap-2 border-b border-black/5 px-3 py-2 dark:border-white/5">
            <Search size={14} className="shrink-0 text-zinc-400" />
            <input
              type="text"
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={searchPlaceholder}
              aria-label={searchPlaceholder}
              className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-zinc-400"
            />
          </div>

          <ul className="max-h-64 overflow-y-auto py-1">
            {/* The current one is listed but not clickable: switching to what you
                are already on is a no-op that looks like it did something. */}
            <li>
              <span
                title={currentHint || undefined}
                className={`${row} cursor-default hover:bg-transparent dark:hover:bg-transparent`}
              >
                <span className="flex-1 truncate">{current}</span>
                <Check size={15} className="shrink-0 text-sky-500" />
              </span>
            </li>
            {shown.map((i) => (
              <li key={i.id}>
                <button
                  type="button"
                  title={i.hint}
                  onClick={() => {
                    setOpen(false);
                    onSelect(i.id);
                  }}
                  className={row}
                >
                  <span className="flex-1 truncate">{i.name}</span>
                </button>
              </li>
            ))}
            {shown.length === 0 && (
              <li className="px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">
                {query.trim() ? "No matches" : emptyLabel}
              </li>
            )}
          </ul>
        </div>
      )}
    </div>
  );
}
