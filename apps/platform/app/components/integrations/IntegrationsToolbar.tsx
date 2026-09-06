"use client";

import Link from "next/link";
import type { RefObject } from "react";
import { Plus, Upload } from "lucide-react";

/**
 * The manager's two top-right actions: import an integration from a file, or
 * start a new one. Either shape imports — a bare `.yaml` definition, or a `.zip`
 * bundle carrying the definition and every resource it owns — and both create a
 * new integration.
 *
 * The hidden file input travels with the button that clicks it — it exists only
 * to be triggered by it, and separating the two leaves a stray input in the tree
 * with nothing nearby to say what it is for.
 *
 * Both shed their labels in a narrow header, after the section nav has shed its
 * own — these are two controls against that nav's nine, so they are the ones with
 * room to spare the longest.
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
    <div className="ml-auto flex shrink-0 items-center gap-2">
      <input
        ref={importInput}
        type="file"
        accept=".yaml,.yml,.zip"
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
        aria-label="Import"
        title="Import"
        className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-md border border-black/10 px-3 py-1 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] hover:text-zinc-900 disabled:opacity-50 dark:border-white/15 dark:text-zinc-300 dark:hover:bg-white/[0.06] dark:hover:text-zinc-100"
      >
        <Upload size={15} />
        <span className="@max-3xl:hidden">Import</span>
      </button>
      <Link
        href="/platform/new"
        aria-label="New integration"
        title="New integration"
        className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
      >
        <Plus size={15} />
        <span className="@max-3xl:hidden">New integration</span>
      </Link>
    </div>
  );
}
