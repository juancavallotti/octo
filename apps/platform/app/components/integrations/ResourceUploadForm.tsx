"use client";

import { useRef, useState } from "react";
import { Upload } from "lucide-react";
import { createResource } from "@/app/model/orchestrator";
import { guessKind, KINDS, type Kind } from "./resources";

/**
 * The resources panel's upload form: pick a file, adjust the name and kind, save.
 *
 * Content always comes from an uploaded file rather than being authored inline;
 * the name is prefilled from the file but stays editable, so a relative path can
 * be set, and the kind follows the name's convention until it is chosen
 * explicitly. Richer editing is left to the (future) editor story.
 *
 * Split from the list beside it because none of this state — the pending file,
 * the draft name, whether the kind was touched — means anything to the list, and
 * a form's state leaking into a list is how a list starts re-rendering on
 * keystrokes.
 */
export default function ResourceUploadForm({
  integrationId,
  onUploaded,
  onError,
}: {
  integrationId: string;
  /** Called after a successful upload, so the list reloads. */
  onUploaded: () => void;
  /** Report a failure to the panel's banner (null clears it). */
  onError: (message: string | null) => void;
}) {
  const [name, setName] = useState("");
  const [kind, setKind] = useState<Kind>("env");
  const [kindTouched, setKindTouched] = useState(false);
  // The uploaded file's text, or null until a file has been read.
  const [content, setContent] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  // Until the user picks a kind explicitly, follow the name's convention.
  const effectiveKind = kindTouched ? kind : guessKind(name);
  const ready = name.trim() !== "" && content !== null && !busy;

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    onError(null);
    try {
      const text = await file.text();
      setContent(text);
      // Prefill the name from the file only when the user hasn't typed one.
      setName((prev) => (prev.trim() === "" ? file.name : prev));
    } catch (e) {
      onError((e as Error).message);
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
    onError(null);
    try {
      await createResource(integrationId, effectiveKind, name.trim(), content);
      reset();
      onUploaded();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
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
  );
}
