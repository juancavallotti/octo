"use client";

import { useEffect, useRef, useState } from "react";
import { Tag } from "lucide-react";
import { useSave } from "@octo/editor";
import { createSnapshot } from "@/app/model/orchestrator";

/**
 * Editor header control that tags the current integration as a version. Tagging
 * saves first (so the snapshot matches what's on screen), then freezes the saved
 * definition under the entered tag. The authoritative integration id comes from
 * `getIntegrationId` — a ref the host updates on save — so we never read a stale
 * id from a closure captured before the save resolves.
 *
 * Renders nothing without a filesystem capability (no save → nothing to tag), and
 * is disabled while there's nothing worth persisting yet (empty document).
 */
export default function TagButton({
  getIntegrationId,
}: {
  getIntegrationId: () => string | null;
}) {
  const save = useSave();
  const [open, setOpen] = useState(false);
  const [tag, setTag] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

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
      setTag("");
      setOpen(false);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => {
          setError(null);
          setOpen((o) => !o);
        }}
        disabled={save.empty}
        title={save.empty ? "Nothing to tag yet" : "Tag this version"}
        aria-haspopup="dialog"
        aria-expanded={open}
        className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-2.5 py-1 text-sm font-medium transition-colors hover:bg-black/[0.04] disabled:opacity-50 dark:border-white/15 dark:hover:bg-white/[0.06]"
      >
        <Tag size={14} />
        Tag
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Tag this version"
          className="absolute right-0 top-full z-30 mt-2 w-64 rounded-xl border border-black/10 bg-white p-3 shadow-lg dark:border-white/10 dark:bg-zinc-900"
        >
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
              if (e.key === "Enter") submit();
            }}
            className="w-full rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
          />
          {error && <p className="mt-1.5 text-xs text-red-500">{error}</p>}
          <div className="mt-2 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setOpen(false)}
              disabled={busy}
              className="rounded-md px-2.5 py-1 text-sm text-zinc-600 hover:bg-black/[0.06] disabled:opacity-50 dark:text-zinc-300 dark:hover:bg-white/[0.08]"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={submit}
              disabled={busy || !tag.trim()}
              className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-2.5 py-1 text-sm font-medium text-white hover:bg-sky-500 disabled:opacity-50"
            >
              <Tag size={13} />
              {busy ? "Saving…" : "Save & tag"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
