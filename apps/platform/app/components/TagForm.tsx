"use client";

import { useEffect, useState } from "react";
import { useSave } from "@octo/editor";
import { createSnapshot, listSnapshots } from "@/app/model/orchestrator";
import { DEFAULT_TAG, suggestNextTag } from "@/app/model/tags";

/**
 * Tags the current integration as a version — the form the header's overflow menu
 * shows on "Tag version…". Tagging saves first (so the snapshot matches what's on
 * screen), then freezes the saved definition under the entered tag. The
 * authoritative integration id comes from `getIntegrationId` — a ref the host
 * updates on save — so we never read a stale id from a closure captured before the
 * save resolved.
 *
 * The popover, its click-outside and its Escape belong to the menu now; this is
 * the field and the two buttons.
 */
export default function TagForm({
  getIntegrationId,
  onDone,
}: {
  getIntegrationId: () => string | null;
  /** Closes the menu this form is shown in. */
  onDone: () => void;
}) {
  const save = useSave();
  // Prefilled with the suggested next version: bump the revision of the
  // integration's highest existing tag (the default when it is unsaved or has
  // none). The user can still edit it before tagging.
  const [tag, setTag] = useState(DEFAULT_TAG);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const id = getIntegrationId();
    if (!id) return;
    let cancelled = false;
    listSnapshots(id).then(
      (snaps) => {
        if (!cancelled) setTag(suggestNextTag(snaps.map((s) => s.tag)));
      },
      () => {},
    );
    return () => {
      cancelled = true;
    };
  }, [getIntegrationId]);

  // No filesystem capability => no tagging (mirrors how Save hides).
  if (!save) return null;

  const submit = async () => {
    const name = tag.trim();
    if (!name || busy) return;
    setBusy(true);
    setError(null);
    try {
      // Save first so the snapshot captures the on-screen definition. A no-op when
      // nothing changed; on the first save it mints the id (read below via the ref).
      await save.save();
      const id = getIntegrationId();
      if (!id) {
        setError("Save the integration before tagging.");
        return;
      }
      await createSnapshot(id, name);
      onDone();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="p-3">
      <label className="mb-1 block text-xs font-medium text-zinc-500">
        Version tag
      </label>
      <input
        autoFocus
        value={tag}
        disabled={busy}
        placeholder="e.g. v1.0"
        onChange={(e) => setTag(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") void submit();
        }}
        className="w-full rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
      />
      {error && <p className="mt-1.5 text-xs text-red-500">{error}</p>}
      <div className="mt-2 flex justify-end gap-2">
        <button
          type="button"
          onClick={onDone}
          disabled={busy}
          className="rounded-md px-2.5 py-1 text-sm text-zinc-600 hover:bg-black/[0.06] disabled:opacity-50 dark:text-zinc-300 dark:hover:bg-white/[0.08]"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={() => void submit()}
          disabled={busy || !tag.trim()}
          className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-2.5 py-1 text-sm font-medium text-white hover:bg-sky-500 disabled:opacity-50"
        >
          {busy ? "Tagging…" : "Tag"}
        </button>
      </div>
    </div>
  );
}
