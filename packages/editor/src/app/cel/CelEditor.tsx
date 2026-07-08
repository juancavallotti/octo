"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Editor from "react-simple-code-editor";
import { highlightCel } from "./highlight";
import {
  applyCompletion,
  completionsAt,
  type CompletionQuery,
} from "./complete";
import type { CelEntry } from "./catalog";

/**
 * A single-field CEL editor: a syntax-highlighted text input (react-simple-code-editor
 * + Prism, mirroring the Resources editor) with a lightweight completion menu and
 * IDE-style hover docs driven by the CEL catalogue. It replaces the plain `<textarea>`
 * used for `cel` fields and `expr` inputs.
 *
 * Completion is deliberately basic (issue #125): prefix-match on the in-scope
 * variables and catalogued functions, triggered as you type (or on Ctrl/Cmd+Space).
 * The menu anchors below the field — caret-accurate placement is a possible follow-up.
 */

const MONO_FONT =
  "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace";

/** react-simple-code-editor copies these onto both the textarea and the highlight
 * <pre> so their glyph metrics line up; the mono font must be set here, not via a
 * class, or the <pre> would fall back to the app's sans font. */
const EDITOR_STYLE: React.CSSProperties = {
  fontFamily: MONO_FONT,
  fontSize: 12,
  lineHeight: 1.6,
};

export interface CelEditorProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  /** Minimum height in px (defaults to ~2 lines). */
  minHeight?: number;
  /** Forwarded to the textarea for label association. */
  id?: string;
  disabled?: boolean;
}

export default function CelEditor({
  value,
  onChange,
  placeholder = "CEL expression",
  minHeight = 40,
  id,
  disabled,
}: CelEditorProps) {
  const [menu, setMenu] = useState<{
    items: CelEntry[];
    query: CompletionQuery;
  } | null>(null);
  const [selected, setSelected] = useState(0);
  const taRef = useRef<HTMLTextAreaElement | null>(null);
  // Set after accepting a completion so the caret lands correctly once value updates.
  const pendingCaret = useRef<number | null>(null);

  // react-simple-code-editor's prop types intersect HTMLAttributes<HTMLDivElement>,
  // so the handler event targets widen to a div|textarea union; at runtime they are
  // always the textarea. Narrow once here.
  const captureTa = useCallback((el: EventTarget & Element) => {
    taRef.current = el as HTMLTextAreaElement;
  }, []);

  const refresh = useCallback(
    (text: string, caret: number, explicit = false) => {
      const { items, query } = completionsAt(text, caret, { explicit });
      if (items.length === 0) {
        setMenu(null);
        return;
      }
      setMenu({ items, query });
      setSelected(0);
    },
    [],
  );

  // Reposition the caret after an accepted completion re-renders with new value.
  useEffect(() => {
    if (pendingCaret.current != null && taRef.current) {
      const pos = pendingCaret.current;
      pendingCaret.current = null;
      taRef.current.focus();
      taRef.current.setSelectionRange(pos, pos);
    }
  });

  const accept = useCallback(
    (entry: CelEntry) => {
      setMenu((m) => {
        if (!m) return null;
        const { text, caret } = applyCompletion(value, m.query, entry);
        pendingCaret.current = caret;
        onChange(text);
        return null;
      });
    },
    [value, onChange],
  );

  const handleValueChange = useCallback(
    (text: string) => {
      onChange(text);
      const caret = taRef.current?.selectionStart ?? text.length;
      refresh(text, caret);
    },
    [onChange, refresh],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement | HTMLDivElement>) => {
      const ta = e.currentTarget as HTMLTextAreaElement;
      captureTa(ta);
      if (menu) {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          setSelected((i) => (i + 1) % menu.items.length);
          return;
        }
        if (e.key === "ArrowUp") {
          e.preventDefault();
          setSelected((i) => (i - 1 + menu.items.length) % menu.items.length);
          return;
        }
        if (e.key === "Enter" || e.key === "Tab") {
          e.preventDefault();
          accept(menu.items[selected]);
          return;
        }
        if (e.key === "Escape") {
          e.preventDefault();
          setMenu(null);
          return;
        }
      }
      // On-demand trigger (Ctrl/Cmd+Space) lists everything at the caret.
      if ((e.ctrlKey || e.metaKey) && e.code === "Space") {
        e.preventDefault();
        refresh(value, ta.selectionStart, true);
      }
    },
    [menu, selected, accept, refresh, value, captureTa],
  );

  const handleKeyUp = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement | HTMLDivElement>) => {
      const ta = e.currentTarget as HTMLTextAreaElement;
      captureTa(ta);
      // Recompute after caret-only moves and deletions (typing is handled by change).
      if (
        e.key === "ArrowLeft" ||
        e.key === "ArrowRight" ||
        e.key === "Backspace" ||
        e.key === "Delete"
      ) {
        refresh(value, ta.selectionStart);
      }
    },
    [refresh, value, captureTa],
  );

  const sel = menu?.items[selected];

  return (
    <div className="relative">
      <div className="rounded-md border border-black/10 bg-transparent focus-within:border-black/30 dark:border-white/15 dark:focus-within:border-white/30">
        <Editor
          value={value}
          onValueChange={handleValueChange}
          highlight={highlightCel}
          onKeyDown={handleKeyDown}
          onKeyUp={handleKeyUp}
          onClick={(e) => {
            captureTa(e.currentTarget);
            setMenu(null);
          }}
          onFocus={(e) => captureTa(e.currentTarget)}
          onBlur={() => setMenu(null)}
          placeholder={placeholder}
          textareaId={id}
          disabled={disabled}
          padding={8}
          preClassName="octo-cel"
          textareaClassName="octo-cel-textarea"
          style={{ ...EDITOR_STYLE, minHeight }}
        />
      </div>

      {menu && (
        <div className="absolute left-0 top-full z-50 mt-1 w-full min-w-[15rem] overflow-hidden rounded-md border border-black/10 bg-white text-sm shadow-lg dark:border-white/15 dark:bg-zinc-900">
          <ul role="listbox" className="max-h-48 overflow-auto py-1">
            {menu.items.map((e, i) => (
              <li
                key={e.name}
                role="option"
                aria-selected={i === selected}
                // preventDefault keeps textarea focus so the click accepts before blur.
                onMouseDown={(ev) => {
                  ev.preventDefault();
                  accept(e);
                }}
                onMouseEnter={() => setSelected(i)}
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
              <div className="mt-0.5 text-zinc-600 dark:text-zinc-300">
                {sel.summary}
              </div>
              <div
                className="mt-1 font-mono text-[11px] text-zinc-400"
                style={{ fontFamily: MONO_FONT }}
              >
                e.g. {sel.example}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
