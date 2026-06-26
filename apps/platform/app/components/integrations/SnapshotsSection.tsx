"use client";

import { useState } from "react";
import { Tag, Trash2 } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  createSnapshot,
  deleteSnapshot,
  type Snapshot,
} from "@/app/model/orchestrator";

/**
 * Version tags for one integration: a create field plus the list of existing
 * tags. A tag freezes the integration's current definition; tags are immutable,
 * so the only mutations are create and delete. The list is owned by the parent
 * (so the Deployments section's change-version menu stays in sync); this section
 * performs the mutations and asks the parent to reload via `onChanged`.
 */
export default function SnapshotsSection({
  integrationId,
  snapshots,
  onChanged,
}: {
  integrationId: string;
  snapshots: Snapshot[];
  onChanged: () => void;
}) {
  const confirm = useConfirm();
  const [tag, setTag] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const create = async () => {
    const name = tag.trim();
    if (!name || busy) return;
    setBusy(true);
    setError(null);
    try {
      await createSnapshot(integrationId, name);
      setTag("");
      onChanged();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

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
      <div className="mb-2 flex gap-2">
        <input
          value={tag}
          disabled={busy}
          placeholder="New tag (e.g. v1.0)"
          onChange={(e) => setTag(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") create();
          }}
          className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
        />
        <button
          type="button"
          onClick={create}
          disabled={busy || !tag.trim()}
          className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
        >
          <Tag size={14} />
          Tag
        </button>
      </div>

      {error && <p className="mb-2 text-sm text-red-500">{error}</p>}

      {snapshots.length === 0 ? (
        <p className="text-sm text-zinc-400">No tags yet.</p>
      ) : (
        <ul className="space-y-1.5">
          {snapshots.map((s) => (
            <li
              key={s.id}
              className="flex items-center gap-2 rounded-md border border-black/10 px-2.5 py-1.5 text-sm dark:border-white/10"
            >
              <Tag size={14} className="shrink-0 text-zinc-400" />
              <span className="min-w-0 flex-1 truncate font-medium">{s.tag}</span>
              <span className="shrink-0 text-xs text-zinc-400">
                {new Date(s.createdAt).toLocaleDateString()}
              </span>
              <button
                type="button"
                onClick={() => remove(s)}
                disabled={busy}
                aria-label={`Delete tag ${s.tag}`}
                className="rounded p-1 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
              >
                <Trash2 size={13} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
