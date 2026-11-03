"use client";

import { useEffect, useState } from "react";

/**
 * localStorage key holding the folders turned away from their default: real
 * folders default open, so a real folder listed here is closed; smart folders
 * default closed, so one listed here is open. Storing the exceptions is what
 * lets a folder created later start open, and keeps the smart folders closed for
 * a viewer whose stored list predates them.
 */
const TOGGLED_KEY = "octo.folderTree.collapsed";

/**
 * Which folders of the tree — smart folders included — are turned away from
 * their default, remembered per viewer. When the selection changes to an
 * integration inside collapsed folders — a deep link, or a selection made
 * elsewhere — those folders open once, so the selected row is never hidden;
 * after that they collapse as usual.
 */
export function useTreeExpansion(
  /** The selected integration's folders, innermost first, once they are known. */
  selection: { id: string; ancestors: string[] } | null,
) {
  const [toggled, setToggled] = useState<ReadonlySet<string>>(new Set());
  const [revealed, setRevealed] = useState<string | null>(null);

  useEffect(() => {
    try {
      const raw = localStorage.getItem(TOGGLED_KEY);
      // Loading persisted UI state on mount is intentional: reading localStorage
      // during render would mismatch the server-rendered (empty) markup, so it has
      // to happen in an effect after hydration.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      if (raw) setToggled(new Set(JSON.parse(raw) as string[]));
    } catch {
      // ignore malformed/blocked storage
    }
  }, []);

  const store = (next: ReadonlySet<string>) => {
    try {
      localStorage.setItem(TOGGLED_KEY, JSON.stringify([...next]));
    } catch {
      // ignore blocked storage
    }
    return next;
  };

  // Adjusted while rendering rather than in an effect, so the selected row never
  // paints hidden for a frame first.
  if (selection && selection.id !== revealed) {
    setRevealed(selection.id);
    if (selection.ancestors.some((id) => toggled.has(id))) {
      const next = new Set(toggled);
      for (const id of selection.ancestors) next.delete(id);
      setToggled(store(next));
    }
  }

  const toggle = (id: string) =>
    setToggled((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return store(next);
    });

  return { toggled, toggle };
}
