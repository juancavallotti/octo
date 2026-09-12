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
  const dialog = useRef<HTMLDivElement>(null);
  /** Whatever had focus when the palette opened, so closing can hand it back. */
  const restoreTo = useRef<HTMLElement | null>(null);

  // Focus on open. Without this the palette appears and swallows nothing: the
  // keystroke that summoned it left focus on the canvas.
  useEffect(() => {
    if (!open) return;
    restoreTo.current = document.activeElement as HTMLElement | null;
    input.current?.focus();
    return () => {
      // And hand it back on close. A dismissed modal that leaves focus on <body>
      // costs the user every keyboard shortcut until they click something.
      restoreTo.current?.focus?.();
    };
  }, [open]);

  /**
   * The highlighted row, clamped into the list.
   *
   * The parent owns `active`, and the list under it changes as the user types — so
   * an index that was valid a keystroke ago can point past the end now. Clamping
   * here rather than trusting the parent is the difference between a filtered list
   * whose Enter does nothing and one that always has a selection: every way the two
   * can drift apart is a way for the palette to look broken.
   *
   * Computed above the early return, not below it, because the scroll effect needs it
   * — an effect cannot sit after a conditional return.
   */
  const activeIndex = items.length === 0 ? -1 : Math.min(Math.max(active, 0), items.length - 1);

  // Keep the highlighted row on screen while the arrows walk past the fold. On the
  // clamped index, not the raw one: out of range is exactly the case the clamp exists
  // for, and `children[active]` is undefined there — so the row the user can see
  // highlighted would be the one row never scrolled to.
  useEffect(() => {
    if (!open || activeIndex < 0) return;
    const row = list.current?.children[activeIndex];
    // Optional call: jsdom has no scrollIntoView, and a palette that throws in the
    // tests to keep a row visible has its priorities backwards.
    (row as HTMLElement | undefined)?.scrollIntoView?.({ block: "nearest" });
  }, [open, activeIndex]);

  if (!open) return null;

  const move = (delta: number) => {
    if (items.length === 0) return;
    onActiveChange((activeIndex + delta + items.length) % items.length);
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
        const item = items[activeIndex];
        if (item !== undefined) onPick(item);
        return;
      }
      case "Tab":
        // `aria-modal` tells a screen reader the rest of the page is inert; nothing
        // makes that true for the Tab key, so the dialog has to hold focus itself.
        // With one focusable child — which is the usual shape here, since the rows are
        // options rather than buttons — this simply keeps the caret in the box.
        e.preventDefault();
        e.stopPropagation();
        cycleFocus(dialog.current, e.shiftKey);
        return;
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
        ref={dialog}
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
          aria-activedescendant={
            items[activeIndex] ? `command-palette-${keyOf(items[activeIndex])}` : undefined
          }
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
                  aria-selected={i === activeIndex}
                  onMouseEnter={() => onActiveChange(i)}
                  onClick={() => onPick(item)}
                >
                  {renderItem(item, i === activeIndex)}
                </div>
              ))}
        </div>
      </div>
    </div>
  );
}

/** What counts as reachable by Tab. */
const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

/** Move focus to the next (or previous) focusable element inside `root`, wrapping. */
function cycleFocus(root: HTMLElement | null, back: boolean): void {
  const all = root ? [...root.querySelectorAll<HTMLElement>(FOCUSABLE)] : [];
  if (all.length === 0) return;
  const at = all.indexOf(document.activeElement as HTMLElement);
  all[(at + (back ? -1 : 1) + all.length) % all.length].focus();
}
