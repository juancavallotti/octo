"use client";

import { useCallback, useMemo, useState } from "react";
import { filterRanked } from "@octo/util";

/**
 * The search field and cursor behind a ranked picker panel.
 *
 * Split out of {@link AppPicker} when a second thing wanted the same panel — the
 * agent's list of past conversations — because what makes that panel usable is
 * not its markup but this: ranking rather than filtering, a cursor that survives
 * narrowing, and the keys someone expects to work in a list. A second copy of it
 * would have been a second set of answers to those questions.
 *
 * It owns no open/closed state. Whether the panel is showing is the caller's
 * business — a toolbar control and an icon button in a header open one for
 * different reasons — and this only says what is in it and where the cursor sits.
 */
export function usePickerSearch<T>({
  items,
  toText,
  onChoose,
  onClose,
}: {
  items: readonly T[];
  /** Everything someone might type to find an item; what the ranking reads. */
  toText: (item: T) => string;
  onChoose: (item: T) => void;
  /** Escape, or Tab out. The caller decides what closing means. */
  onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);

  const matches = useMemo(
    () => filterRanked(items, query, toText),
    [items, query, toText],
  );

  // Clamped as it is read, not corrected afterwards in an effect: narrowing the
  // list must not leave a render pointing past the end of it, and the cursor is
  // held rather than reset so retyping narrows *under* it.
  const active = Math.min(cursor, Math.max(matches.length - 1, 0));

  const reset = useCallback(() => {
    setQuery("");
    setCursor(0);
  }, []);

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
        if (matches[active]) onChoose(matches[active]);
        break;
      case "Escape":
        e.preventDefault();
        onClose();
        break;
      case "Tab":
        // Tabbing out closes rather than trapping — a picker is not a dialog.
        //
        // The default is deliberately *not* cancelled. The caller moves focus to
        // whatever opened the panel and the browser then continues the Tab from
        // there, so forward still goes forward and Shift+Tab still goes back.
        // Cancelling it made Tab walk backwards, since the trigger precedes the
        // field the key was pressed in. Doing nothing at all is not an option
        // either: closing unmounts that field in the same commit, and focus would
        // fall to the body.
        onClose();
        break;
    }
  };

  return { query, setQuery, matches, active, setCursor, onKeyDown, reset };
}
