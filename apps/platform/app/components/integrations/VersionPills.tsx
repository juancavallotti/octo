"use client";

import { useState } from "react";
import { Tag, X } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { deleteSnapshot, type Snapshot } from "@/app/model/orchestrator";

/**
 * The version tags for one integration, rendered as a compact single row of pills:
 * green when the tag is deployed somewhere, grey otherwise. Clicking a pill selects
 * it as the active version (the header dropdown reflects it, scoping the
 * Resources/Env panels). A deployed tag can't be deleted — the orchestrator refuses
 * to drop a version a live deployment still pins — so its delete affordance is
 * hidden. Deletes ask the parent to reload via `onChanged`.
 */

/** Pill colour by deployed state; `deletable` trims the right padding so the ✕
 * button sits flush. No active highlight — the header dropdown owns that. */
function pillClass(deployed: boolean, deletable: boolean): string {
  const tone = deployed
    ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300"
    : "bg-zinc-500/10 text-zinc-600 dark:text-zinc-300";
  const pad = deletable ? "pl-2.5 pr-1" : "px-2.5";
  return `inline-flex items-center gap-1 rounded-full py-0.5 text-xs font-medium ${tone} ${pad}`;
}

export default function VersionPills({
  snapshots,
  deployedTags,
  onSelectTag,
  onChanged,
}: {
  snapshots: Snapshot[];
  /** Tags currently deployed somewhere; these read green and can't be deleted. */
  deployedTags: ReadonlySet<string>;
  /** Select this tag as the active version (or null for the working copy). */
  onSelectTag: (tag: string | null) => void;
  onChanged: () => void;
}) {
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const remove = async (s: Snapshot) => {
    const ok = await confirm({
      title: `Delete tag "${s.tag}"?`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    setError(null);
    try {
      await deleteSnapshot(s.id);
      onChanged();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="flex flex-wrap gap-1.5">
        {snapshots.map((s) => {
          const deployed = deployedTags.has(s.tag);
          return (
            <span
              key={s.id}
              title={deployed ? "Deployed" : undefined}
              className={pillClass(deployed, !deployed)}
            >
              <button
                type="button"
                onClick={() => onSelectTag(s.tag)}
                className="inline-flex items-center gap-1"
              >
                <Tag size={11} className="shrink-0 opacity-70" />
                {s.tag}
              </button>
              {!deployed && (
                <button
                  type="button"
                  onClick={() => remove(s)}
                  disabled={busy}
                  aria-label={`Delete tag ${s.tag}`}
                  className="rounded-full p-0.5 opacity-60 transition-colors hover:bg-red-500/15 hover:text-red-500 hover:opacity-100 disabled:opacity-40"
                >
                  <X size={11} />
                </button>
              )}
            </span>
          );
        })}
      </div>
      {error && <p className="mt-1 text-xs text-red-500">{error}</p>}
    </>
  );
}
