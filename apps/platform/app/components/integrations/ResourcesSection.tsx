"use client";

import { useCallback, useEffect, useState } from "react";
import { Download, FileText, Lock, Trash2 } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  deleteResource,
  listResources,
  listSnapshotResources,
  snapshotResourceContent,
} from "@/app/model/orchestrator";
import { downloadResource } from "./files";
import ResourceUploadForm from "./ResourceUploadForm";
import { fromFrozen, fromLive, type DisplayResource } from "./resources";

/**
 * Resources (env files, templates) for one integration: the upload form, and the
 * list of existing resources — each downloadable on its own. The section owns its
 * own data, loading it by integration id; the form beside it owns the pending
 * upload.
 *
 * When a version is selected (`snapshotId` set), it instead shows that tag's
 * frozen resources read-only — a snapshot is immutable, so upload and delete are
 * hidden. Downloading stays available: reading a frozen file changes nothing.
 *
 * The whole integration downloads and uploads as one archive from the header;
 * this panel is the per-file view of the same thing.
 */
export default function ResourcesSection({
  integrationId,
  snapshotId,
  versionLabel,
}: {
  integrationId: string;
  /** When set, show this tag's frozen resources (read-only) instead of the live
   * working-copy set. */
  snapshotId?: string;
  /** The selected version's tag, for the read-only banner. */
  versionLabel?: string;
}) {
  const confirm = useConfirm();
  const frozen = snapshotId != null;
  const [resources, setResources] = useState<DisplayResource[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load the live working-copy resources, or a tag's frozen set when a version is
  // selected. Both normalize to DisplayResource; frozen entries have no id.
  const reload = useCallback(() => {
    if (snapshotId != null) {
      listSnapshotResources(snapshotId).then(
        (rs) => setResources(rs.map(fromFrozen)),
        () => setResources([]),
      );
    } else {
      listResources(integrationId).then(
        (rs) => setResources(rs.map(fromLive)),
        () => setResources([]),
      );
    }
  }, [integrationId, snapshotId]);
  useEffect(() => {
    reload();
  }, [reload]);

  // Download one resource under its own name. A live resource's content came with
  // the list, so it saves without a round trip; a frozen one is metadata only, so
  // its bytes are fetched from the snapshot first.
  const download = async (r: DisplayResource) => {
    setError(null);
    try {
      if (r.content !== undefined) {
        downloadResource(r.name, r.content);
        return;
      }
      if (snapshotId == null) return;
      const bytes = await snapshotResourceContent(snapshotId, r.kind, r.name);
      downloadResource(r.name, bytes as BlobPart);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const remove = async (r: DisplayResource) => {
    if (!r.id) return; // frozen resources are read-only
    const ok = await confirm({
      title: `Delete resource "${r.name}"?`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    setError(null);
    try {
      await deleteResource(integrationId, r.id);
      reload();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      {frozen ? (
        <p className="mb-2 flex items-center gap-1.5 text-xs text-zinc-400">
          <Lock size={12} className="shrink-0" />
          Frozen at {versionLabel ?? "this version"} — read-only. Select
          “Current” to edit.
        </p>
      ) : (
        <ResourceUploadForm
          integrationId={integrationId}
          onUploaded={reload}
          onError={setError}
        />
      )}

      {error && <p className="mb-2 text-sm text-red-500">{error}</p>}

      {resources.length === 0 ? (
        <p className="text-sm text-zinc-400">
          {frozen
            ? "No resources frozen in this version."
            : "No resources yet."}
        </p>
      ) : (
        <ul className="space-y-1.5">
          {resources.map((r) => (
            <li
              key={r.key}
              className="flex items-center gap-2 rounded-md border border-black/10 px-2.5 py-1.5 text-sm dark:border-white/10"
            >
              <FileText size={14} className="shrink-0 text-zinc-400" />
              <span className="min-w-0 flex-1 truncate font-medium">
                {r.name}
              </span>
              <span className="shrink-0 rounded bg-black/5 px-1.5 py-0.5 text-xs font-medium text-zinc-500 dark:bg-white/10 dark:text-zinc-400">
                {r.kind}
              </span>
              <button
                type="button"
                onClick={() => download(r)}
                aria-label={`Download resource ${r.name}`}
                title="Download"
                className="rounded p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 dark:hover:bg-white/10 dark:hover:text-zinc-200"
              >
                <Download size={13} />
              </button>
              {!frozen && (
                <button
                  type="button"
                  onClick={() => remove(r)}
                  disabled={busy}
                  aria-label={`Delete resource ${r.name}`}
                  className="rounded p-1 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
                >
                  <Trash2 size={13} />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
