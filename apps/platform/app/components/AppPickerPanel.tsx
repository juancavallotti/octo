"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { Check, Search } from "lucide-react";

/**
 * The open half of {@link AppPicker}: a search field over a ranked list.
 *
 * Split out because the closed control and the open panel answer different
 * questions — one shows what is chosen, the other shows what could be — and only
 * this half has to think about a cursor, a scroll position, and what an empty
 * list means. It owns no state: the cursor lives with the keyboard handler that
 * moves it, one level up.
 */
export function AppPickerPanel<T>({
  matches,
  sourceCount,
  active,
  onActivate,
  onChoose,
  selectedKey,
  toKey,
  renderRow,
  query,
  onQueryChange,
  onKeyDown,
  idPrefix,
  label,
  placeholder,
  empty,
  notice,
  align = "left",
}: {
  matches: readonly T[];
  /** How many items there were before the query, so "none" can say which none. */
  sourceCount: number;
  active: number;
  onActivate: (index: number) => void;
  onChoose: (item: T) => void;
  selectedKey: string | null;
  toKey: (item: T) => string;
  renderRow: (item: T) => ReactNode;
  query: string;
  onQueryChange: (query: string) => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
  idPrefix: string;
  label: string;
  placeholder: string;
  empty?: ReactNode;
  /**
   * Something that went wrong while the panel was open, shown under the search
   * field. It is not an empty state: the list is still there and still usable,
   * and taking it away to show the message would lose what the panel is for.
   */
  notice?: ReactNode;
  /**
   * Which edge the panel hangs from. A trigger at the right of its container — an
   * icon in a header — would otherwise open a panel running off the screen.
   */
  align?: "left" | "right";
}) {
  const search = useRef<HTMLInputElement>(null);
  const listbox = useRef<HTMLDivElement>(null);

  useEffect(() => {
    search.current?.focus();
  }, []);

  // Keeping the cursor visible is the listbox's job, not the row's: the rows are
  // the caller's markup and should not have to know they are inside a picker.
  useEffect(() => {
    const option = listbox.current?.querySelector<HTMLElement>('[data-active="true"]');
    // Guarded on the method rather than the element: jsdom has no scrollIntoView.
    option?.scrollIntoView?.({ block: "nearest" });
  }, [active, matches.length]);

  return (
    <div
      className={`absolute top-full z-40 mt-1 w-[min(28rem,90vw)] overflow-hidden rounded-md border border-black/10 bg-white shadow-lg dark:border-white/15 dark:bg-zinc-900 ${
        align === "right" ? "right-0" : "left-0"
      }`}
    >
      <div className="flex items-center gap-2 border-b border-black/10 px-2 py-1.5 dark:border-white/10">
        <Search size={13} className="shrink-0 text-zinc-400" aria-hidden />
        <input
          ref={search}
          role="combobox"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder={placeholder}
          aria-label={`Search ${label.toLowerCase()}`}
          aria-controls={`${idPrefix}-listbox`}
          aria-expanded
          aria-activedescendant={matches[active] ? optionId(idPrefix, active) : undefined}
          autoComplete="off"
          className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-zinc-400"
        />
      </div>

      {notice && (
        <p className="border-b border-black/10 bg-red-500/[0.06] px-3 py-1.5 text-[11px] text-red-500 dark:border-white/10">
          {notice}
        </p>
      )}

      <div
        ref={listbox}
        id={`${idPrefix}-listbox`}
        role="listbox"
        aria-label={label}
        className="max-h-80 overflow-y-auto"
      >
        {matches.length === 0 ? (
          // "Nothing to look at" and "nothing matched what you typed" are
          // different problems with different fixes, so they get different words.
          <p className="px-3 py-4 text-xs text-zinc-400">
            {sourceCount === 0
              ? (empty ?? "Nothing to choose from.")
              : "Nothing matches what you typed."}
          </p>
        ) : (
          matches.map((item, index) => {
            const key = toKey(item);
            const isSelected = selectedKey === key;
            return (
              <div
                key={key}
                id={optionId(idPrefix, index)}
                role="option"
                aria-selected={isSelected}
                data-active={index === active}
                // The pointer must not take focus off the search field, or the
                // keyboard stops working the moment the mouse is used once.
                onPointerDown={(e) => e.preventDefault()}
                onClick={() => onChoose(item)}
                onMouseMove={() => onActivate(index)}
                className={`flex cursor-pointer items-start gap-2 border-b border-black/[0.04] px-3 py-2 dark:border-white/[0.06] ${
                  index === active ? "bg-black/[0.04] dark:bg-white/[0.06]" : ""
                } ${isSelected ? "bg-sky-500/10" : ""}`}
              >
                <span className="min-w-0 flex-1">{renderRow(item)}</span>
                {isSelected && (
                  <Check size={13} className="mt-0.5 shrink-0 text-sky-500" aria-hidden />
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

/**
 * The DOM id of one option.
 *
 * By position rather than by key, because this is where the id is minted and so
 * this is where its rules apply: `aria-activedescendant` holds a single ID
 * reference and an ID reference cannot contain whitespace, while a caller's key
 * is free to — traces keys a row by "<deployment> <version>". Callers should not
 * have to know that their key ends up in an attribute with a grammar.
 */
function optionId(prefix: string, index: number): string {
  return `${prefix}-option-${index}`;
}
