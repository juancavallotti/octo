"use client";

import { useEffect, useState } from "react";
import { getSnapshot } from "@/app/model/orchestrator";

/**
 * A version tag's frozen definition, read when the tag is selected. The list of
 * tags carries no definitions — merging every version's config to fill a picker
 * grew with the history — so the one being looked at is read on its own.
 *
 * Null while it is being read, and when it could not be: the working copy is
 * never shown in its place, because that would present the wrong version's
 * numbers as the tag's.
 */
export function useSnapshotDefinition(snapshotId: string | undefined): string | null {
  const [read, setRead] = useState<{ id: string; definition: string } | null>(null);

  useEffect(() => {
    if (!snapshotId) return;
    let live = true;
    getSnapshot(snapshotId).then(
      (s) => live && setRead({ id: s.id, definition: s.definition }),
      () => {},
    );
    return () => {
      live = false;
    };
  }, [snapshotId]);

  // Matched by id, so switching tags never shows the previous tag's definition
  // while the next one is on its way.
  return snapshotId && read?.id === snapshotId ? read.definition : null;
}
