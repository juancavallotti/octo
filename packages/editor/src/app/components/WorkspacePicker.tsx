"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { FolderOpen, Search } from "lucide-react";

/**
 * "What am I working on, and what else could I be working on" — the chip at the
 * left of the header, next to the mark, and the menu it opens.
 *
 * The items are whatever the caller is switching between — folders, integrations — and
 * the shape is the same either way: a current thing, a list of others, pick to switch.
 *
 * `actions` are the optional rows above the list (e.g. "Open folder…"); pass none and
 * there are none. `groups` are further lists under a heading of their own, below the
 * main one, which the search filters with it.
 */

export interface PickerItem {
  /** Stable key, and what `onSelect` is called with. */
  id: string;
  name: string;
  /** Optional hover text, e.g. a folder's absolute path, shown at the menu's foot. */
  hint?: string;
  /** Optional mark before the name, e.g. the icon an integration chose. */
  icon?: React.ReactNode;
  /** Optional mark after the name, e.g. that a folder is linked to the platform. */
  badge?: React.ReactNode;
  /** Optional button at the end of the row, doing something other than switching to it. */
  action?: PickerRowAction;
}

export interface PickerAction {
  label: string;
  icon?: React.ReactNode;
  onClick(): void;
}

/** A row's own button: an icon, named for the screen reader and the tooltip. */
export interface PickerRowAction {
  label: string;
  icon: React.ReactNode;
  onClick(): void;
  disabled?: boolean;
}

export interface PickerGroup {
  /** Stable key for the group. */
  id: string;
  /** The heading: text, or text with an icon. */
  label: React.ReactNode;
  items: PickerItem[];
  /** Shown when the group has nothing in it and no search is narrowing it. */
  emptyLabel?: string;
}

export default function WorkspacePicker({
  label,
  current,
  currentHint,
  currentId,
  items,
  groups = [],
  onSelect,
  onOpen,
  actions = [],
  icon,
  searchPlaceholder = "Search…",
  emptyLabel = "Nothing else here",
}: {
  /** Accessible name for the chip, e.g. "Project folder" / "Integration". */
  label: string;
  /** The thing being worked on now. */
  current: string;
  currentHint?: string;
  /**
   * The current one's id, when it is also one of the items: that row is then marked
   * as current where it is, with its badge and action, rather than listed on its own.
   */
  currentId?: string;
  items: PickerItem[];
  groups?: PickerGroup[];
  /** Called with the id of the item picked, from the main list or a group. */
  onSelect(id: string): void;
  /** Called when the menu opens, for a host that refreshes its list then. */
  onOpen?(): void;
  actions?: PickerAction[];
  /** The chip's icon. Absent means a folder, the thing most hosts switch between. */
  icon?: React.ReactNode;
  searchPlaceholder?: string;
  emptyLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  // The hovered row's hint. Shown at the menu's foot rather than as a native title
  // tooltip: on macOS an Electron tooltip swallows wheel events, so the list would
  // stop scrolling as soon as one came up.
  const [hint, setHint] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    onOpen?.();
    // Fresh search each time: a filter left over from the last visit hides things
    // the user has no reason to think are hidden.
    setQuery("");
    setHint(null);
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

  const q = query.trim().toLowerCase();
  const shown = useMemo(() => (q ? items.filter((i) => i.name.toLowerCase().includes(q)) : items), [items, q]);
  const shownGroups = useMemo(
    () => groups.map((g) => ({ ...g, items: q ? g.items.filter((i) => i.name.toLowerCase().includes(q)) : g.items })),
    [groups, q],
  );

  const row = "flex w-full items-center gap-2 px-3 py-2 text-left text-sm";
  const hover = "transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]";
  // The current one is listed but not clickable: switching to what you are already
  // on is a no-op that looks like it did something.
  const currentRow = "bg-sky-500/10 font-medium text-sky-700 dark:text-sky-300";
  const listed = currentId !== undefined && [...items, ...groups.flatMap((g) => g.items)].some((i) => i.id === currentId);

  const rowAction = (action: PickerRowAction) => (
    <button
      type="button"
      aria-label={action.label}
      disabled={action.disabled}
      onMouseEnter={() => setHint(action.label)}
      onClick={() => {
        setOpen(false);
        action.onClick();
      }}
      className="mr-1.5 shrink-0 rounded p-1 transition-colors hover:bg-black/[0.06] disabled:opacity-40 dark:hover:bg-white/[0.08]"
    >
      {action.icon}
    </button>
  );

  const itemRow = (i: PickerItem) => (
    <li
      key={i.id}
      onMouseEnter={() => setHint(i.hint ?? null)}
      className={`flex items-center ${i.id === currentId ? currentRow : hover}`}
    >
      {i.id === currentId ? (
        <span aria-current="true" className={`${row} min-w-0 flex-1 cursor-default`}>
          {i.icon}
          <span className="flex-1 truncate">{i.name}</span>
          {i.badge}
        </span>
      ) : (
        <button
          type="button"
          onClick={() => {
            setOpen(false);
            onSelect(i.id);
          }}
          className={`${row} min-w-0 flex-1`}
        >
          {i.icon}
          <span className="flex-1 truncate">{i.name}</span>
          {i.badge}
        </button>
      )}
      {i.action && rowAction(i.action)}
    </li>
  );

  return (
    // min-w-0 down the chain: the picker is what gives way in a narrow header, its
    // name truncating before any control is pushed off the bar.
    <div ref={ref} className="relative min-w-0">
      <button
        type="button"
        title={currentHint || undefined}
        aria-label={label}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex min-w-0 max-w-full items-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm text-zinc-600 transition-colors hover:border-black/10 hover:text-zinc-900 dark:text-zinc-300 dark:hover:border-white/15 dark:hover:text-zinc-100"
      >
        {icon ?? <FolderOpen size={14} className="shrink-0 text-zinc-400" />}
        <span className="min-w-0 max-w-[10rem] truncate">{current}</span>
      </button>

      {open && (
        /* Left-anchored, under the chip: it sits at the left of the header, where
           there is room for the menu to open below it. */
        <div className="absolute left-0 top-full z-50 mt-2 w-72 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          {actions.length > 0 && (
            <div className="border-b border-black/5 py-1 dark:border-white/5">
              {actions.map((action) => (
                <button
                  key={action.label}
                  type="button"
                  onClick={() => {
                    setOpen(false);
                    action.onClick();
                  }}
                  className={row}
                >
                  {action.icon}
                  <span className="flex-1">{action.label}</span>
                </button>
              ))}
            </div>
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

          {/* As tall as the window allows, so it only scrolls when it has to; a
              scroll that reaches either end stops there rather than carrying on
              to the page. */}
          <ul
            onMouseLeave={() => setHint(null)}
            className="max-h-[calc(100vh-14rem)] overflow-y-auto overscroll-contain py-1"
          >
            {!listed && (
              <li className={currentRow} onMouseEnter={() => setHint(currentHint ?? null)}>
                <span aria-current="true" className={`${row} cursor-default`}>
                  <span className="flex-1 truncate">{current}</span>
                </span>
              </li>
            )}
            {shown.map(itemRow)}
            {shown.length === 0 && (
              <li className="px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">{q ? "No matches" : emptyLabel}</li>
            )}
            {shownGroups.map((g) => (
              <li key={g.id} className="mt-1 border-t border-black/5 dark:border-white/5">
                {/* Pinned while its rows scroll under it, so a long list still says
                    whose it is. */}
                <div className="sticky top-0 z-10 flex items-center gap-1.5 bg-white px-3 pb-1 pt-2 text-xs text-zinc-500 dark:bg-zinc-900 dark:text-zinc-400">
                  {g.label}
                </div>
                <ul>
                  {g.items.map(itemRow)}
                  {g.items.length === 0 && (
                    <li className="px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">
                      {q ? "No matches" : (g.emptyLabel ?? "Nothing here")}
                    </li>
                  )}
                </ul>
              </li>
            ))}
          </ul>
          <div className="h-7 truncate border-t border-black/5 px-3 py-1.5 text-[11px] text-zinc-400 dark:border-white/5 dark:text-zinc-500">
            {hint}
          </div>
        </div>
      )}
    </div>
  );
}
