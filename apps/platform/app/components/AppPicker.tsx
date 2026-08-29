"use client";

import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { ChevronDown, RefreshCw } from "lucide-react";
import { filterRanked } from "@octo/util";
import { AppPickerPanel } from "./AppPickerPanel";

/**
 * Choosing which app to look at, once, for every page that asks.
 *
 * The platform had grown two answers to that question and neither was good. The
 * traces page spent a permanent column on a master list, which is a lot of the
 * window to give a choice someone makes once and then stops thinking about. The
 * object store and the memory viewer used a native `<select>`, which cannot be
 * searched — and a deployment list is exactly the kind of list where someone
 * knows the name and not the position — and then followed it with a *second*
 * `<select>` for whatever scoped the first, which reads as a form to fill in
 * rather than as a thing to point at.
 *
 * So: one control, in the toolbar, that opens a searchable list. Typing ranks
 * rather than filters, so a dropped letter or a wrong key still finds the app
 * instead of emptying the list.
 *
 * Two deliberate omissions. It holds no selection — the caller does, in whatever
 * it already uses (the traces path, the object store's query string, memory's
 * state), because where a selection lives is a decision about being linkable, not
 * about being picked. And it renders no rows of its own: `renderRow` gets the
 * whole row, because what identifies an app differs per page — a deployment and
 * its version and its cost here, an integration name there — and a picker that
 * flattened all of that to a label would lose the part someone chooses by.
 *
 * The second dropdown becomes {@link AppPickerProps.accessory}: still available,
 * no longer a gate. Where the second axis is really part of the app's identity —
 * a rollout's version — it belongs in the row instead, and traces does that.
 */
export interface AppPickerProps<T> {
  items: readonly T[];
  /** The chosen item, or null when nothing is chosen yet. */
  selected: T | null;
  onSelect: (item: T) => void;
  toKey: (item: T) => string;
  /** Everything someone might type to find this item; what the ranking reads. */
  toText: (item: T) => string;
  renderRow: (item: T) => ReactNode;
  /** The trigger's face. Falls back to {@link AppPickerProps.toText}. */
  renderValue?: (item: T) => ReactNode;
  /**
   * Leading slot in the toolbar, for a control the choice below depends on.
   *
   * It is before the picker rather than after it because reading order is claim
   * order: traces puts the window here, and the window is what the app list was
   * counted over — offered afterwards it asks someone to choose from a list
   * already narrowed by something they have not been shown yet.
   */
  leading?: ReactNode;
  /** Trailing slot in the toolbar — where a second axis goes, if there is one. */
  accessory?: ReactNode;
  /** Names the control for a screen reader: "Application". */
  label: string;
  placeholder?: string;
  /** Shown in place of the list when there is nothing to choose from at all. */
  empty?: ReactNode;
  loading?: boolean;
  onRefresh?: () => void;
}

export function AppPicker<T>({
  items,
  selected,
  onSelect,
  toKey,
  toText,
  renderRow,
  renderValue,
  leading,
  accessory,
  label,
  placeholder = "Search…",
  empty,
  loading = false,
  onRefresh,
}: AppPickerProps<T>) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);

  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const id = useId();

  const matches = useMemo(
    () => filterRanked(items, query, toText),
    [items, query, toText],
  );

  // Clamped as it is read, not corrected afterwards in an effect: narrowing the
  // list must not leave a render pointing past the end of it, and the cursor is
  // held rather than reset so retyping narrows *under* it.
  const active = Math.min(cursor, Math.max(matches.length - 1, 0));

  const close = useCallback((restoreFocus: boolean) => {
    setOpen(false);
    setQuery("");
    if (restoreFocus) trigger.current?.focus();
  }, []);

  // Opening is what clears the query, not closing: a list that rebuilds itself
  // while it is going away is a list that flickers.
  const openPanel = useCallback(() => {
    setQuery("");
    setCursor(0);
    setOpen(true);
  }, []);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) close(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open, close]);

  const choose = useCallback(
    (item: T) => {
      onSelect(item);
      close(true);
    },
    [onSelect, close],
  );

  const onKeyDown = (e: React.KeyboardEvent) => {
    const count = matches.length;
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setCursor(count ? (active + 1) % count : 0);
        break;
      case "ArrowUp":
        e.preventDefault();
        setCursor(count ? (active - 1 + count) % count : 0);
        break;
      case "Home":
        e.preventDefault();
        setCursor(0);
        break;
      case "End":
        e.preventDefault();
        setCursor(Math.max(count - 1, 0));
        break;
      case "Enter":
        e.preventDefault();
        if (matches[active]) choose(matches[active]);
        break;
      case "Escape":
        e.preventDefault();
        close(true);
        break;
      case "Tab":
        // Tabbing out closes rather than trapping — a picker is not a dialog.
        // Focus goes back to the trigger rather than onward: closing unmounts
        // the search field in the same commit, so letting the browser move from
        // it drops focus to the body and the next Tab restarts from the top of
        // the page instead of reaching the accessory.
        e.preventDefault();
        close(true);
        break;
    }
  };

  const face = selected ? (renderValue ?? toText)(selected) : null;

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-black/10 px-4 py-2.5 dark:border-white/10">
      {leading}

      {/* A basis rather than a bare flex-1: with a leading control beside it the
          trigger was shrinking to a single letter of the app's name, and the
          toolbar already wraps — a picker on its own line reads, a picker
          crushed to "D." does not. */}
      <div ref={root} className="relative min-w-0 max-w-md flex-1 basis-48">
        <button
          ref={trigger}
          type="button"
          onClick={() => (open ? close(false) : openPanel())}
          aria-label={label}
          aria-haspopup="listbox"
          aria-expanded={open}
          className="flex w-full items-center gap-2 rounded-md border border-black/10 bg-transparent px-2 py-1 text-left text-sm transition-colors hover:bg-black/[0.03] dark:border-white/15 dark:hover:bg-white/[0.04]"
        >
          <span className="min-w-0 flex-1 truncate">
            {face ?? <span className="text-zinc-400">{placeholder}</span>}
          </span>
          <ChevronDown size={14} className="shrink-0 text-zinc-400" aria-hidden />
        </button>

        {open && (
          <AppPickerPanel
            matches={matches}
            sourceCount={items.length}
            active={active}
            onActivate={setCursor}
            onChoose={choose}
            selectedKey={selected ? toKey(selected) : null}
            toKey={toKey}
            renderRow={renderRow}
            query={query}
            onQueryChange={setQuery}
            onKeyDown={onKeyDown}
            idPrefix={id}
            label={label}
            placeholder={placeholder}
            // Loading wins over the caller's text: an empty list mid-fetch is
            // not yet the empty list their message describes, and most of those
            // messages name a cause ("tracing is off by default") that would be
            // a guess before anything has arrived.
            empty={loading ? "Loading…" : empty}
          />
        )}
      </div>

      {accessory}

      {onRefresh && (
        <button
          type="button"
          onClick={onRefresh}
          aria-label="Refresh"
          className="ml-auto shrink-0 rounded p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 dark:hover:bg-white/[0.08] dark:hover:text-zinc-200"
        >
          <RefreshCw size={13} className={loading ? "animate-spin" : undefined} />
        </button>
      )}
    </div>
  );
}
