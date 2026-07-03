"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { FileText, Trash2, Upload } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  createResource,
  deleteResource,
  listResources,
  type Resource,
} from "@/app/model/orchestrator";

/**
 * Resources (env files, templates) for one integration: upload a file plus the
 * list of existing resources. Content always comes from an uploaded file rather
 * than being authored inline; the name is prefilled from the file (editable, so a
 * relative path can be set) and the kind is guessed from the name. Richer editing
 * is left to the (future) editor story. The section owns its own data, loading it
 * by integration id.
 */

const KINDS = ["env", "template"] as const;
type Kind = (typeof KINDS)[number];

// Guess a resource's kind from its name the way the standalone loader does: a
// `.env`-convention file is env, everything else is a template.
function guessKind(name: string): Kind {
  const base = name.split("/").pop() ?? name;
  return base.startsWith(".env") ? "env" : "template";
}

export default function ResourcesSection({
  integrationId,
}: {
  integrationId: string;
}) {
  const confirm = useConfirm();
  const [resources, setResources] = useState<Resource[]>([]);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<Kind>("env");
  const [kindTouched, setKindTouched] = useState(false);
  // The uploaded file's text, or null until a file has been read.
  const [content, setContent] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);

  const reload = useCallback(() => {
    listResources(integrationId).then(setResources, () => setResources([]));
  }, [integrationId]);
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

  const remove = async (r: Resource) => {
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

      {error && <p className="mb-2 text-sm text-red-500">{error}</p>}

      {resources.length === 0 ? (
        <p className="text-sm text-zinc-400">No resources yet.</p>
      ) : (
        <ul className="space-y-1.5">
          {resources.map((r) => (
            <li
              key={r.id}
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
                onClick={() => remove(r)}
                disabled={busy}
                aria-label={`Delete resource ${r.name}`}
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
