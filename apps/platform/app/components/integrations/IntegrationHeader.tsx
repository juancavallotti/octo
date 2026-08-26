"use client";

import { useRef, useState, type RefObject } from "react";
import Link from "next/link";
import { Copy, Pencil, Rocket, Trash2, Upload } from "lucide-react";
import type { Integration, Snapshot } from "@/app/model/orchestrator";
import DownloadMenu from "./DownloadMenu";
import VersionMenu from "./VersionMenu";
import { downloadDefinition } from "./files";

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
  activeSnapshot,
  deployedTags,
  busy,
  effectiveTag,
  replaceInput,
  onSelectTag,
  onRename,
  onDownloadBundle,
  onReplaceFromBundle,
  onDeploy,
  onCopy,
  onDelete,
}: {
  integration: Integration;
  snapshots: Snapshot[];
  /** The tag the pane is scoped to, or null for the working copy. Its frozen
   * definition is what the definition download offers. */
  activeSnapshot: Snapshot | null;
  deployedTags: ReadonlySet<string>;
  busy: boolean;
  /** The version the pane is scoped to; null is the working copy. */
  effectiveTag: string | null;
  /** Hidden file input backing "Replace from bundle", owned by the manager so the
   * upload runs through the same busy/error plumbing as every other mutation. */
  replaceInput: RefObject<HTMLInputElement | null>;
  onSelectTag: (tag: string | null) => void;
  /** Returns whether the rename was accepted; false keeps the field open. */
  onRename: (name: string) => Promise<boolean>;
  /** Download a version as a bundle archive: the active tag's frozen contents,
   * or the working copy when it is null. */
  onDownloadBundle: (snapshot: { id: string; tag: string } | null) => void;
  /** Overwrite this integration's contents from a picked bundle. */
  onReplaceFromBundle: (file: File) => void;
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
      {/* Export, scoped to the active version: the definition alone, or the whole
          integration (definition + every resource) as a bundle. A tag exports its
          frozen definition, not the working copy's. */}
      <DownloadMenu
        versionLabel={effectiveTag ?? "Current"}
        busy={busy}
        onDownloadDefinition={() =>
          downloadDefinition(
            integration.name,
            activeSnapshot?.definition ?? integration.definition,
          )
        }
        onDownloadBundle={() =>
          onDownloadBundle(
            activeSnapshot && effectiveTag
              ? { id: activeSnapshot.id, tag: effectiveTag }
              : null,
          )
        }
      />
      {/* Import the other way: overwrite this integration from a bundle. Only for
          the working copy — a tag is immutable — and hidden rather than disabled
          on one, since there is nothing to switch back to but Current. */}
      {effectiveTag === null && (
        <>
          <input
            ref={replaceInput}
            type="file"
            accept=".zip,application/zip"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0];
              // Cleared first: picking the same file twice must fire onChange again.
              e.target.value = "";
              if (file) onReplaceFromBundle(file);
            }}
          />
          <button
            type="button"
            onClick={() => replaceInput.current?.click()}
            disabled={busy}
            aria-label="Replace from bundle"
            title="Replace from bundle"
            className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/10 dark:hover:text-zinc-200"
          >
            <Upload size={16} />
          </button>
        </>
      )}
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
