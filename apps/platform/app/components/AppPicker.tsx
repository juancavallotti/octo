"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { ReactNode } from "react";
import { ChevronDown, RefreshCw } from "lucide-react";
import { AppPickerPanel } from "./AppPickerPanel";
import { usePickerSearch } from "./usePickerSearch";

/**
 * Choosing which app to look at, once, for every page that asks: one control, in
 * the toolbar, that opens a searchable list. Typing ranks rather than filters, so
 * a dropped letter or a wrong key still finds the app instead of emptying the
 * list.
 *
 * Two omissions. It holds no selection — the caller does, because where a
 * selection lives is a decision about being linkable, not about being picked. And
 * it renders no rows of its own: `renderRow` gets the whole row, because what
 * identifies an app differs per page, and a picker that flattened all of that to a
 * label would lose the part someone chooses by.
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
   * Before the picker rather than after it because reading order is claim order:
   * offered afterwards, it asks someone to choose from a list already narrowed by
   * something they have not been shown yet.
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

  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const id = useId();

  const close = useCallback((restoreFocus: boolean) => {
    setOpen(false);
    if (restoreFocus) trigger.current?.focus();
  }, []);

  const choose = useCallback(
    (item: T) => {
      onSelect(item);
      close(true);
    },
    [onSelect, close],
  );

  const search = usePickerSearch({
    items,
    toText,
    onChoose: choose,
    onClose: () => close(true),
  });
  const { query, matches, active, reset } = search;

  // Opening is what clears the query, not closing: a list that rebuilds itself
  // while it is going away is a list that flickers.
  const openPanel = useCallback(() => {
    reset();
    setOpen(true);
  }, [reset]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) close(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open, close]);

  const face = selected ? (renderValue ?? toText)(selected) : null;

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-black/10 px-4 py-2.5 dark:border-white/10">
      {leading}

      {/* A basis rather than a bare flex-1: with a leading control beside it the
          trigger shrinks to a single letter of the app's name, and the toolbar
          already wraps — a picker on its own line reads, a picker crushed to
          "D." does not. */}
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
            onActivate={search.setCursor}
            onChoose={choose}
            selectedKey={selected ? toKey(selected) : null}
            toKey={toKey}
            renderRow={renderRow}
            query={query}
            onQueryChange={search.setQuery}
            onKeyDown={search.onKeyDown}
            idPrefix={id}
            label={label}
            placeholder={placeholder}
            // Loading wins over the caller's text: an empty list mid-fetch is
            // not yet the empty list their message describes, and a message that
            // names a cause would be a guess before anything has arrived.
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
