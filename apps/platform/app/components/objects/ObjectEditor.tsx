"use client";

import { Database, Lock, Save, Trash2 } from "lucide-react";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";

import type { ObjectEntry, ObjectValue } from "@/app/model/objects";
import type { Action } from "./state";

/**
 * The right-hand pane: what one object is, and the editing of it.
 *
 * Three states in one component because they are three renderings of one thing —
 * nothing selected, a secret key (listed and deletable, never readable), and a
 * value being viewed or written. Splitting them would mean three components that
 * only ever appear in the same slot and share the same header.
 */
export default function ObjectEditor({
  selectedKey,
  selectedEntry,
  current,
  draft,
  creating,
  newKey,
  binary,
  dirty,
  secret,
  busy,
  dispatch,
  onSave,
  onRemove,
}: {
  selectedKey: string | null;
  selectedEntry: ObjectEntry | null;
  current: ObjectValue | null;
  draft: string;
  creating: boolean;
  newKey: string;
  /** The value is base64; shown read-only rather than risk a lossy text edit. */
  binary: boolean;
  dirty: boolean;
  /** A secret namespace: browse and delete only, never view or edit. */
  secret: boolean;
  busy: boolean;
  dispatch: (action: Action) => void;
  onSave: () => void;
  onRemove: () => void;
}) {
  return (
    <section className="flex min-h-0 flex-col overflow-y-auto">
      {creating ? (
        <div className="flex min-h-0 flex-1 flex-col gap-3 p-4">
          <input
            autoFocus
            value={newKey}
            onChange={(e) =>
              dispatch({ type: "setNewKey", value: e.target.value })
            }
            placeholder="key (may contain slashes)"
            className="rounded-md border border-black/10 bg-transparent px-2.5 py-1.5 font-mono text-sm dark:border-white/15"
          />
          <textarea
            value={draft}
            onChange={(e) =>
              dispatch({ type: "setDraft", value: e.target.value })
            }
            placeholder="value"
            className="min-h-0 flex-1 resize-none rounded-md border border-black/10 bg-transparent p-3 font-mono text-sm dark:border-white/15"
          />
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={() => dispatch({ type: "cancelCreate" })}
              className="rounded-md px-3 py-1.5 text-sm text-zinc-600 transition-colors hover:bg-black/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.08]"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={onSave}
              disabled={busy || !newKey.trim()}
              className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
            >
              <Save size={14} />
              Create
            </button>
          </div>
        </div>
      ) : secret && selectedKey ? (
        <div className="flex min-h-0 flex-1 flex-col gap-3 p-4">
          <div className="flex items-center gap-2">
            <Lock size={13} className="shrink-0 text-amber-500" />
            <h2 className="min-w-0 flex-1 truncate font-mono text-sm font-semibold">
              {selectedKey}
            </h2>
            {selectedEntry && (
              <span className="shrink-0 text-xs text-zinc-400">
                v{selectedEntry.version}
              </span>
            )}
            <button
              type="button"
              onClick={onRemove}
              disabled={busy}
              className="inline-flex items-center gap-1.5 rounded-md border border-red-500/30 px-2.5 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-500/10 disabled:opacity-50 dark:text-red-400"
            >
              <Trash2 size={13} />
              Delete
            </button>
          </div>
          <p className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
            Secret value hidden. Secrets can&apos;t be viewed or edited here —
            delete this key to clean it up (e.g. stale OAuth credentials).
          </p>
        </div>
      ) : current ? (
        <div className="flex min-h-0 flex-1 flex-col gap-3 p-4">
          <div className="flex items-center gap-2">
            <h2 className="min-w-0 flex-1 truncate font-mono text-sm font-semibold">
              {current.key}
            </h2>
            <span className="shrink-0 text-xs text-zinc-400">
              v{current.version}
            </span>
            <button
              type="button"
              onClick={onRemove}
              disabled={busy}
              className="inline-flex items-center gap-1.5 rounded-md border border-red-500/30 px-2.5 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-500/10 disabled:opacity-50 dark:text-red-400"
            >
              <Trash2 size={13} />
              Delete
            </button>
          </div>

          {binary && (
            <p className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
              Binary value (shown base64-encoded, read-only).
            </p>
          )}

          <textarea
            value={draft}
            onChange={(e) =>
              dispatch({ type: "setDraft", value: e.target.value })
            }
            readOnly={binary}
            spellCheck={false}
            className="min-h-0 flex-1 resize-none rounded-md border border-black/10 bg-transparent p-3 font-mono text-sm read-only:opacity-70 dark:border-white/15"
          />

          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={onSave}
              disabled={busy || !dirty}
              className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
            >
              <Save size={14} />
              Save
            </button>
          </div>
        </div>
      ) : (
        <div className="px-6 py-8">
          <EmptyState
            icon={Database}
            title="No object selected"
            body={
              secret
                ? "Select a key on the left to clean it up. Secret values can't be viewed or edited here."
                : "Select a key on the left to view its value, or create a new one."
            }
          />
        </div>
      )}
    </section>
  );
}
