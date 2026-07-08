"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  applyCompletion,
  completionsAt,
  type CompletionQuery,
  type MemberProvider,
} from "./complete";
import { caretCoordinates } from "./caretCoords";
import { autoPair } from "./autoPair";
import type { CelEntry } from "./catalog";

/**
 * Where in a text field CEL may be completed, given the caret. Return the [start,
 * end) offset range CEL occupies, or null when the caret is not in a CEL context
 * (so the menu closes). Lets the same completion machinery serve a whole-field CEL
 * input and CEL embedded inside `{{ }}` template expressions.
 */
export type CelScope = (
  text: string,
  caret: number,
) => { start: number; end: number } | null;

/** The whole field is CEL (used by the standalone CEL field editor). */
export const WHOLE_TEXT_SCOPE: CelScope = (text) => ({
  start: 0,
  end: text.length,
});

/**
 * The `{{ }}` region the caret sits inside, or null. Handles an unclosed `{{` by
 * extending the region to the next `}}` or end of text, so completion works while
 * the expression is still being typed.
 */
export const handlebarsScopeAt: CelScope = (text, caret) => {
  const open = text.lastIndexOf("{{", Math.max(0, caret - 1));
  if (open < 0) return null;
  // A `}}` between the opening braces and the caret means the caret is outside.
  if (text.slice(open + 2, caret).includes("}}")) return null;
  const close = text.indexOf("}}", caret);
  return { start: open + 2, end: close < 0 ? text.length : close };
};

interface MenuState {
  items: CelEntry[];
  /** Absolute range in the full text that an accepted completion replaces. */
  query: CompletionQuery;
  /** Caret pixel position to anchor the menu at, or null to anchor below the field. */
  pos: { left: number; top: number } | null;
}

/**
 * Drives CEL completion for a react-simple-code-editor textarea: tracks the caret,
 * computes suggestions within `scope`, handles keyboard navigation, and applies an
 * accepted entry. Returns handlers to spread onto the Editor plus the current menu
 * state (render with {@link CompletionMenu}).
 */
export function useCelCompletion({
  value,
  onChange,
  scope,
  members,
}: {
  value: string;
  onChange: (value: string) => void;
  scope: CelScope;
  /** Optional member-completion source (e.g. keys from the CEL tester's JSON samples). */
  members?: MemberProvider;
}) {
  const [menu, setMenu] = useState<MenuState | null>(null);
  const [selected, setSelected] = useState(0);
  const taRef = useRef<HTMLTextAreaElement | null>(null);
  // Set after accepting a completion so the caret lands correctly once value updates.
  const pendingCaret = useRef<number | null>(null);

  const close = useCallback(() => setMenu(null), []);

  const captureTa = useCallback((el: EventTarget & Element) => {
    taRef.current = el as HTMLTextAreaElement;
  }, []);

  const refresh = useCallback(
    (text: string, caret: number, explicit = false) => {
      const region = scope(text, caret);
      if (!region || caret < region.start || caret > region.end) {
        setMenu(null);
        return;
      }
      const local = text.slice(region.start, region.end);
      const { items, query } = completionsAt(local, caret - region.start, {
        explicit,
        members,
      });
      if (items.length === 0) {
        setMenu(null);
        return;
      }
      // Anchor at the caret in viewport coordinates so the menu can be rendered in a
      // portal (position: fixed) and escape any clipping/overflow ancestor — e.g. the
      // scrolling settings panel. Falls back to (0,0) when unmeasurable (jsdom).
      let pos: MenuState["pos"] = null;
      if (taRef.current) {
        const rect = taRef.current.getBoundingClientRect();
        const c = caretCoordinates(taRef.current, caret);
        pos = { left: rect.left + c.left, top: rect.top + c.top + c.height + 4 };
      }
      // Map the query range back to absolute offsets so accept splices the full text.
      setMenu({
        items,
        pos,
        query: {
          ...query,
          range: [region.start + query.range[0], region.start + query.range[1]],
        },
      });
      setSelected(0);
    },
    [scope, members],
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

  const onValueChange = useCallback(
    (text: string) => {
      onChange(text);
      const caret = taRef.current?.selectionStart ?? text.length;
      refresh(text, caret);
    },
    [onChange, refresh],
  );

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement | HTMLDivElement>) => {
      const ta = e.currentTarget as HTMLTextAreaElement;
      captureTa(ta);
      // Bracket/quote auto-pairing takes precedence over normal insertion, but only
      // inside CEL scope — in a template's prose (outside `{{ }}`) typing a quote in
      // "don't" must not insert a stray closer. WHOLE_TEXT_SCOPE keeps it always on
      // for standalone CEL fields. Enter/Tab aren't handled by autoPair, so completion
      // accept below still wins for those.
      const region = scope(value, ta.selectionStart);
      const inScope =
        !!region &&
        ta.selectionStart >= region.start &&
        ta.selectionEnd <= region.end;
      const paired = inScope
        ? autoPair(value, ta.selectionStart, ta.selectionEnd, e.key)
        : null;
      if (paired) {
        e.preventDefault();
        pendingCaret.current = paired.caret;
        onChange(paired.value);
        setMenu(null);
        return;
      }
      if (menu) {
        const NAV = ["ArrowDown", "ArrowUp", "Enter", "Tab", "Escape"];
        if (NAV.includes(e.key)) {
          e.preventDefault();
          // Keep an open menu's keys from reaching outer handlers (e.g. a modal's
          // Escape-to-close), so the first Escape only dismisses the menu.
          e.stopPropagation();
        }
        if (e.key === "ArrowDown") {
          setSelected((i) => (i + 1) % menu.items.length);
          return;
        }
        if (e.key === "ArrowUp") {
          setSelected((i) => (i - 1 + menu.items.length) % menu.items.length);
          return;
        }
        if (e.key === "Enter" || e.key === "Tab") {
          accept(menu.items[selected]);
          return;
        }
        if (e.key === "Escape") {
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
    [menu, selected, accept, refresh, value, onChange, captureTa, scope],
  );

  const onKeyUp = useCallback(
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

  const onClick = useCallback(
    (e: React.MouseEvent<HTMLTextAreaElement | HTMLDivElement>) => {
      captureTa(e.currentTarget);
      setMenu(null);
    },
    [captureTa],
  );

  const onFocus = useCallback(
    (e: React.FocusEvent<HTMLTextAreaElement | HTMLDivElement>) => {
      captureTa(e.currentTarget);
    },
    [captureTa],
  );

  return {
    menu,
    selected,
    setSelected,
    accept,
    close,
    handlers: { onValueChange, onKeyDown, onKeyUp, onClick, onFocus, onBlur: close },
  };
}
