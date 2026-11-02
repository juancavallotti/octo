"use client";

import { useCallback, useRef } from "react";

/**
 * For a section handed its first answer by the pane: returns a check that is
 * true exactly once, for the read it was handed, so the section's effect skips
 * that read and makes every later one as before.
 */
export function useSeed(seeded: boolean): () => boolean {
  const pending = useRef(seeded);
  return useCallback(() => {
    const handed = pending.current;
    pending.current = false;
    return handed;
  }, []);
}
