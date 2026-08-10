"use client";

import Link from "next/link";
import type { RefObject } from "react";
import { Plus, Upload } from "lucide-react";

/**
 * The manager's two top-right actions: import a .yaml, or start a new
 * integration.
 *
 * The hidden file input travels with the button that clicks it — it exists only
 * to be triggered by it, and separating the two leaves a stray input in the tree
 * with nothing nearby to say what it is for.
 */
export default function IntegrationsToolbar({
  importInput,
  busy,
  onImportFile,
}: {
  importInput: RefObject<HTMLInputElement | null>;
  busy: boolean;
  onImportFile: (file: File) => void;
}) {
  return (
    <div className="ml-auto flex items-center gap-2">
      <input
        ref={importInput}
        type="file"
        accept=".yaml,.yml"
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0];
          // Reset first so re-selecting the same file fires onChange again.
          e.target.value = "";
          if (file) onImportFile(file);
        }}
      />
      <button
        type="button"
        onClick={() => importInput.current?.click()}
        disabled={busy}
        className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-3 py-1 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] hover:text-zinc-900 disabled:opacity-50 dark:border-white/15 dark:text-zinc-300 dark:hover:bg-white/[0.06] dark:hover:text-zinc-100"
      >
        <Upload size={15} />
        Import
      </button>
      <Link
        href="/platform/new"
        className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
      >
        <Plus size={15} />
        New integration
      </Link>
    </div>
  );
}
