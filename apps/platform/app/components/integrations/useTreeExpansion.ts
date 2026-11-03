"use client";

import { useEffect, useState } from "react";
import { smartKey } from "./treeModel";

/** localStorage key holding the ids of collapsed folders (so new folders default open). */
const COLLAPSED_KEY = "octo.folderTree.collapsed";

/**
 * Which folders of the tree — smart folders included — are collapsed,
 * remembered per viewer.
 *
 * Collapsed ids are stored rather than expanded ones so a folder created later
 * starts open. When the selection changes to an integration inside collapsed
 * folders — a deep link, or a selection made elsewhere — those folders open once,
 * so the selected row is never hidden; after that they collapse as usual.
 */
export function useTreeExpansion(
  /** The selected integration's folders, innermost first, once they are known. */
  selection: { id: string; ancestors: string[] } | null,
) {
  // All integrations repeats the whole tree, so it starts closed; everything
  // else starts open.
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(
    () => new Set([smartKey("all")]),
  );
  const [revealed, setRevealed] = useState<string | null>(null);

  useEffect(() => {
    try {
      const raw = localStorage.getItem(COLLAPSED_KEY);
      // Loading persisted UI state on mount is intentional: reading localStorage
      // during render would mismatch the server-rendered (empty) markup, so it has
      // to happen in an effect after hydration.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      if (raw) setCollapsed(new Set(JSON.parse(raw) as string[]));
    } catch {
      // ignore malformed/blocked storage
    }
  }, []);

  const store = (next: ReadonlySet<string>) => {
    try {
      localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...next]));
    } catch {
      // ignore blocked storage
    }
    return next;
  };

  // Adjusted while rendering rather than in an effect, so the selected row never
  // paints hidden for a frame first.
  if (selection && selection.id !== revealed) {
    setRevealed(selection.id);
    if (selection.ancestors.some((id) => collapsed.has(id))) {
      const next = new Set(collapsed);
      for (const id of selection.ancestors) next.delete(id);
      setCollapsed(store(next));
    }
  }

  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return store(next);
    });

  return { collapsed, toggle };
}
