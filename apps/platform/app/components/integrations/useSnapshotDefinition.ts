"use client";

import { getSnapshot } from "@/app/model/orchestrator";
import { useLoadable, type Loadable } from "@/app/components/ui/loadable";

/**
 * A version tag's frozen definition, read when the tag is selected. The list of
 * tags carries no definitions — merging every version's config to fill a picker
 * grew with the history — so the one being looked at is read on its own.
 *
 * Keyed by the tag, so switching tags starts from loading and never shows the
 * previous tag's definition; and never the working copy in its place, which would
 * present the wrong version's numbers as the tag's. With no tag it stays loading
 * and reads nothing.
 */
export function useSnapshotDefinition(snapshotId: string | undefined): Loadable<string> {
  return useLoadable(snapshotId ?? null, async () => {
    const snapshot = await getSnapshot(snapshotId ?? "");
    return snapshot.definition;
  }).state;
}
