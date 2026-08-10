"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { FileText, Lock, Trash2, Upload } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  createResource,
  deleteResource,
  listResources,
  listSnapshotResources,
} from "@/app/model/orchestrator";
import {
  fromFrozen,
  fromLive,
  guessKind,
  KINDS,
  type DisplayResource,
  type Kind,
} from "./resources";

/**
 * Resources (env files, templates) for one integration: upload a file plus the
 * list of existing resources. Content always comes from an uploaded file rather
 * than being authored inline; the name is prefilled from the file (editable, so a
 * relative path can be set) and the kind is guessed from the name. Richer editing
 * is left to the (future) editor story. The section owns its own data, loading it
 * by integration id.
 *
 * When a version is selected (`snapshotId` set), it instead shows that tag's
 * frozen resources read-only — a snapshot is immutable, so upload/delete are
 * hidden and only the name/kind metadata is listed.
 */

/** A resource for display: a live resource (has an id, deletable) or a frozen
 * snapshot resource (metadata only). */
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
  const [name, setName] = useState("");
  const [kind, setKind] = useState<Kind>("env");
  const [kindTouched, setKindTouched] = useState(false);
  // The uploaded file's text, or null until a file has been read.
  const [content, setContent] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);

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

  // Until the user picks a kind explicitly, follow the name's convention.
  const effectiveKind = kindTouched ? kind : guessKind(name);
  const ready = name.trim() !== "" && content !== null && !busy;

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    setError(null);
    try {
      const text = await file.text();
      setContent(text);
      // Prefill the name from the file only when the user hasn't typed one.
      setName((prev) => (prev.trim() === "" ? file.name : prev));
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const reset = () => {
    setName("");
    setContent(null);
    setKindTouched(false);
    if (fileInput.current) fileInput.current.value = "";
  };

  const upload = async () => {
    if (!ready || content === null) return;
    setBusy(true);
    setError(null);
    try {
      await createResource(integrationId, effectiveKind, name.trim(), content);
      reset();
      reload();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
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
          Frozen at {versionLabel ?? "this version"} — read-only. Select “Current”
          to edit.
        </p>
      ) : (
        <div className="mb-2 space-y-2">
        <input
          ref={fileInput}
          type="file"
          disabled={busy}
          onChange={(e) => onFile(e.target.files?.[0])}
          className="block w-full text-sm text-zinc-500 file:mr-3 file:rounded-md file:border-0 file:bg-black/5 file:px-3 file:py-1 file:text-sm file:font-medium file:text-zinc-700 hover:file:bg-black/10 dark:text-zinc-400 dark:file:bg-white/10 dark:file:text-zinc-200 dark:hover:file:bg-white/15"
        />
        <div className="flex gap-2">
          <input
            value={name}
            disabled={busy}
            placeholder="Name (e.g. .env.dev or templates/welcome.tmpl)"
            onChange={(e) => setName(e.target.value)}
            className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
          />
          <select
            value={effectiveKind}
            disabled={busy}
            aria-label="Resource kind"
            onChange={(e) => {
              setKindTouched(true);
              setKind(e.target.value as Kind);
            }}
            className="rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
          >
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>
        </div>
        <button
          type="button"
          onClick={upload}
          disabled={!ready}
          className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
        >
          <Upload size={14} />
          Upload
        </button>
        </div>
      )}

      {error && <p className="mb-2 text-sm text-red-500">{error}</p>}

      {resources.length === 0 ? (
        <p className="text-sm text-zinc-400">
          {frozen ? "No resources frozen in this version." : "No resources yet."}
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
