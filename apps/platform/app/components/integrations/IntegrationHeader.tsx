"use client";

import { useRef, useState } from "react";
import Link from "next/link";
import { Copy, Download, Pencil, Rocket, Trash2 } from "lucide-react";
import type { Integration, Snapshot } from "@/app/model/orchestrator";
import VersionMenu from "./VersionMenu";
import { downloadDefinition } from "./yamlFile";

/**
 * The detail pane's header: the integration's name, edited in place, and the
 * Deploy control beside it.
 *
 * It owns the rename's editing state — the draft, whether the field is open, and
 * the ref that lets Escape cancel without committing — because nothing outside
 * the header can see any of it. The parent supplies only `onRename`, and the
 * boolean it returns is what keeps the field open on a rejected name.
 */
export default function IntegrationHeader({
  integration,
  snapshots,
  deployedTags,
  busy,
  effectiveTag,
  onSelectTag,
  onRename,
  onDeploy,
  onCopy,
  onDelete,
}: {
  integration: Integration;
  snapshots: Snapshot[];
  deployedTags: ReadonlySet<string>;
  busy: boolean;
  /** The version the pane is scoped to; null is the working copy. */
  effectiveTag: string | null;
  onSelectTag: (tag: string | null) => void;
  /** Returns whether the rename was accepted; false keeps the field open. */
  onRename: (name: string) => Promise<boolean>;
  onDeploy: () => void;
  onCopy: () => void;
  onDelete: () => void;
}) {
  const [editingName, setEditingName] = useState(false);
  const [nameDraft, setNameDraft] = useState(integration.name);
  const cancelRename = useRef(false);
  const nameInput = useRef<HTMLInputElement>(null);
  const commitRename = async () => {
    if (cancelRename.current) {
      cancelRename.current = false;
      setEditingName(false);
      return;
    }
    const name = nameDraft.trim();
    if (!name || name === integration.name) {
      setEditingName(false);
      return;
    }
    // Keep the editor open until the rename is accepted; a rejected name (e.g. a
    // duplicate) leaves the field open and re-focused so it can be corrected. The
    // error itself is surfaced by the parent's inline banner.
    const ok = await onRename(name);
    if (ok) setEditingName(false);
    else nameInput.current?.focus();
  };

  return (
    <header className="flex items-center gap-2 px-4 py-3">
      {editingName ? (
        <input
          ref={nameInput}
          autoFocus
          value={nameDraft}
          disabled={busy}
          aria-label="Integration name"
          onChange={(e) => setNameDraft(e.target.value)}
          onBlur={commitRename}
          onKeyDown={(e) => {
            if (e.key === "Enter") e.currentTarget.blur();
            else if (e.key === "Escape") {
              cancelRename.current = true;
              e.currentTarget.blur();
            }
          }}
          className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-1.5 py-0.5 text-base font-semibold outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
        />
      ) : (
        <button
          type="button"
          onClick={() => {
            setNameDraft(integration.name);
            setEditingName(true);
          }}
          title="Rename integration"
          className="min-w-0 flex-1 truncate text-left text-base font-semibold hover:underline"
        >
          {integration.name}
        </button>
      )}
      {/* Active version: scopes the Resources/Env panels, the pills below, the
          deployments filter, and the deploy target. Always available (Current is
          always a choice, even with no tags yet). */}
      <VersionMenu
        snapshots={snapshots}
        deployedTags={deployedTags}
        value={effectiveTag}
        onChange={onSelectTag}
      />
      <button
        type="button"
        onClick={onDeploy}
        disabled={busy}
        className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
      >
        <Rocket size={14} />
        Deploy
      </button>
      {/* Edit opens the working copy; disabled (not hidden) when a frozen tag is
          the active version — switch to Current to edit. Green so it reads as the
          primary "work on this" action next to the sky Deploy. */}
      {effectiveTag === null ? (
        <Link
          href={`/platform/i/${encodeURIComponent(integration.id)}`}
          className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-emerald-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-emerald-500"
        >
          <Pencil size={14} />
          Edit
        </Link>
      ) : (
        <button
          type="button"
          disabled
          title="Switch to Current to edit the working copy"
          className="inline-flex shrink-0 cursor-not-allowed items-center gap-1.5 rounded-md bg-emerald-600/40 px-3 py-1 text-sm font-medium text-white/70"
        >
          <Pencil size={14} />
          Edit
        </button>
      )}
      <button
        type="button"
        onClick={onCopy}
        disabled={busy}
        aria-label="Duplicate integration"
        title="Duplicate integration"
        className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/10 dark:hover:text-zinc-200"
      >
        <Copy size={16} />
      </button>
      <button
        type="button"
        onClick={() =>
          downloadDefinition(integration.name, integration.definition)
        }
        aria-label="Download integration YAML"
        title="Download YAML"
        className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 dark:hover:bg-white/10 dark:hover:text-zinc-200"
      >
        <Download size={16} />
      </button>
      <button
        type="button"
        onClick={onDelete}
        disabled={busy}
        aria-label="Delete integration"
        className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
      >
        <Trash2 size={16} />
      </button>
    </header>
  );
}
