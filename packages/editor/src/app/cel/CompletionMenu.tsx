"use client";

import type { CelEntry } from "./catalog";

const MONO_FONT =
  "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace";

/**
 * The CEL completion dropdown: a scrollable list of entries plus a doc panel for
 * the highlighted one (signature, summary, example) — the IDE-style hover docs.
 * Purely presentational; it is positioned by its container (`position: relative`)
 * and driven by {@link useCelCompletion}. Shared by the CEL field editor and the
 * template `{{ }}` completion.
 */
export default function CompletionMenu({
  items,
  selected,
  onHover,
  onAccept,
  position,
}: {
  items: CelEntry[];
  selected: number;
  onHover: (index: number) => void;
  onAccept: (entry: CelEntry) => void;
  /** Caret pixel anchor; when omitted the menu sits below the full-width field. */
  position?: { left: number; top: number } | null;
}) {
  const sel = items[selected];
  // At a caret anchor the menu is auto-width; otherwise it spans the field below it.
  const placement = position
    ? "min-w-[15rem] max-w-[22rem]"
    : "left-0 top-full mt-1 w-full min-w-[15rem]";
  return (
    <div
      style={position ? { left: position.left, top: position.top } : undefined}
      className={`absolute z-50 overflow-hidden rounded-md border border-black/10 bg-white text-sm shadow-lg dark:border-white/15 dark:bg-zinc-900 ${placement}`}
    >
      <ul role="listbox" className="max-h-48 overflow-auto py-1">
        {items.map((e, i) => (
          <li
            key={e.name}
            role="option"
            aria-selected={i === selected}
            // preventDefault keeps textarea focus so the click accepts before blur.
            onMouseDown={(ev) => {
              ev.preventDefault();
              onAccept(e);
            }}
            onMouseEnter={() => onHover(i)}
            className={`flex cursor-pointer items-baseline gap-2 px-2 py-1 ${
              i === selected ? "bg-sky-500/15" : ""
            }`}
          >
            <span
              className="font-mono text-zinc-800 dark:text-zinc-100"
              style={{ fontFamily: MONO_FONT }}
            >
              {e.name}
            </span>
            <span className="ml-auto truncate pl-2 text-xs text-zinc-400">
              {e.kind === "variable" ? e.signature : "fn"}
            </span>
          </li>
        ))}
      </ul>
      {sel && (
        <div className="border-t border-black/10 px-2 py-1.5 text-xs dark:border-white/15">
          <div
            className="font-mono text-zinc-500 dark:text-zinc-400"
            style={{ fontFamily: MONO_FONT }}
          >
            {sel.signature}
          </div>
          <div className="mt-0.5 text-zinc-600 dark:text-zinc-300">{sel.summary}</div>
          <div
            className="mt-1 font-mono text-[11px] text-zinc-400"
            style={{ fontFamily: MONO_FONT }}
          >
            e.g. {sel.example}
          </div>
        </div>
      )}
    </div>
  );
}
