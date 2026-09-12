"use client";

import { useEffect, useRef, type ReactNode } from "react";

/**
 * A centered search-and-pick overlay — the Finder/Spotlight shape.
 *
 * Not built on Popover next door, which anchors a panel to the trigger that opened it.
 * This one has no trigger: it is summoned by a keystroke from wherever the user was,
 * so it belongs to the window rather than to a control, and the two dismissal rules
 * are the only thing the two have in common.
 *
 * Controlled, like every primitive here: the parent owns `open`, `query` and which row
 * is highlighted, because the parent is the one that has to close this after a pick.
 */
export interface CommandPaletteProps<T> {
  open: boolean;
  onClose(): void;
  query: string;
  onQueryChange(query: string): void;
  items: T[];
  /** Index of the highlighted row; the parent moves it with the arrow keys. */
  active: number;
  onActiveChange(index: number): void;
  onPick(item: T): void;
  renderItem(item: T, active: boolean): ReactNode;
  keyOf(item: T): string;
  placeholder?: string;
  label: string;
  /** Shown in place of the list when nothing matches. */
  empty?: ReactNode;
}

export default function CommandPalette<T>({
  open,
  onClose,
  query,
  onQueryChange,
  items,
  active,
  onActiveChange,
  onPick,
  renderItem,
  keyOf,
  placeholder,
  label,
  empty,
}: CommandPaletteProps<T>) {
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);

  // Focus on open. Without this the palette appears and swallows nothing: the
  // keystroke that summoned it left focus on the canvas.
  useEffect(() => {
    if (open) input.current?.focus();
  }, [open]);

  // Keep the highlighted row on screen while the arrows walk past the fold.
  useEffect(() => {
    if (!open) return;
    const row = list.current?.children[active];
    // Optional call: jsdom has no scrollIntoView, and a palette that throws in the
    // tests to keep a row visible has its priorities backwards.
    (row as HTMLElement | undefined)?.scrollIntoView?.({ block: "nearest" });
  }, [open, active]);

  if (!open) return null;

  const move = (delta: number) => {
    if (items.length === 0) return;
    onActiveChange((active + delta + items.length) % items.length);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    // Every key handled here is one an outer listener would also claim — Escape
    // closes a selection, Enter submits something — so none of them may escape.
    switch (e.key) {
      case "Escape":
        e.preventDefault();
        e.stopPropagation();
        onClose();
        return;
      case "ArrowDown":
        e.preventDefault();
        e.stopPropagation();
        move(1);
        return;
      case "ArrowUp":
        e.preventDefault();
        e.stopPropagation();
        move(-1);
        return;
      case "Enter": {
        e.preventDefault();
        e.stopPropagation();
        const item = items[active];
        if (item !== undefined) onPick(item);
        return;
      }
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-[12vh]"
      // A click on the backdrop is a dismissal; a click inside is not, which is why
      // this compares the target rather than relying on the panel to stop it.
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={label}
        onKeyDown={onKeyDown}
        className="w-full max-w-lg overflow-hidden rounded-xl border border-black/10 bg-white shadow-2xl dark:border-white/10 dark:bg-zinc-900"
      >
        <input
          ref={input}
          type="text"
          role="combobox"
          aria-expanded
          aria-controls="command-palette-list"
          aria-activedescendant={items[active] ? `command-palette-${keyOf(items[active])}` : undefined}
          aria-label={label}
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder={placeholder}
          className="w-full border-b border-black/10 bg-transparent px-4 py-3 text-sm outline-none dark:border-white/10"
        />
        <div id="command-palette-list" role="listbox" ref={list} className="max-h-80 overflow-y-auto p-2">
          {items.length === 0
            ? empty
            : items.map((item, i) => (
                <div
                  key={keyOf(item)}
                  id={`command-palette-${keyOf(item)}`}
                  role="option"
                  aria-selected={i === active}
                  onMouseEnter={() => onActiveChange(i)}
                  onClick={() => onPick(item)}
                >
                  {renderItem(item, i === active)}
                </div>
              ))}
        </div>
      </div>
    </div>
  );
}
