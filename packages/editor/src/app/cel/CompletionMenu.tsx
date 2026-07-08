"use client";

import { createPortal } from "react-dom";
import type { CelEntry } from "./catalog";

const MONO_FONT =
  "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace";

const MENU_WIDTH = 288; // px — matches w-72; used to clamp against the viewport edge.

/**
 * The CEL completion dropdown: a scrollable list of entries plus a doc panel for
 * the highlighted one (signature, summary, example) — the IDE-style hover docs.
 * Purely presentational and driven by {@link useCelCompletion}.
 *
 * It renders in a portal with `position: fixed` at the caret's viewport coordinates
 * so it escapes any clipping/overflow ancestor (the settings panel scrolls, which
 * would otherwise cut the menu off). Coordinates are clamped to stay on-screen.
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
  /** Caret viewport anchor (fixed positioning); null anchors at the top-left. */
  position?: { left: number; top: number } | null;
}) {
  if (typeof document === "undefined") return null;
  const sel = items[selected];
  const vw = typeof window !== "undefined" ? window.innerWidth : MENU_WIDTH;
  const left = Math.max(
    8,
    Math.min(position?.left ?? 8, vw - MENU_WIDTH - 8),
  );
  const top = position?.top ?? 8;

  return createPortal(
    <div
      style={{ position: "fixed", left, top, width: MENU_WIDTH }}
      className="z-[9999] overflow-hidden rounded-md border border-black/10 bg-white text-sm shadow-lg dark:border-white/15 dark:bg-zinc-900"
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
    </div>,
    document.body,
  );
}
