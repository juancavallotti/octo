"use client";

import { Database, FolderTree, Lock, Plus, RefreshCw } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import { relativeAge } from "@/app/lib/relativeAge";
import { deploymentLabel } from "./format";
import { useObjects } from "./useObjects";
import ObjectEditor from "./ObjectEditor";

/**
 * The object store browser (`/platform/objects`): pick a deployment, list the keys
 * it holds in the user-facing object namespace, and view / edit / create / delete
 * their values. The selected deployment and key are mirrored to the URL so a
 * specific object is bookmarkable. Writes use the stored version for optimistic
 * concurrency; a stale write surfaces the orchestrator's conflict. The interlinked
 * selection/editing state lives in a reducer (./state) and the fetching plus URL
 * sync in useObjects; this component renders.
 */
export default function ObjectsManager() {
  const { available, ready } = useOrchestrator();
  const confirm = useConfirm();

  const {
    deploymentId,
    namespace,
    selectedKey,
    sortedDeployments,
    namespaces,
    entries,
    selectedEntry,
    current,
    draft,
    creating,
    newKey,
    binary,
    dirty,
    secret,
    busy,
    error,
    dispatch,
    reload,
    selectDeployment,
    selectNamespace,
    selectKey,
    startCreate,
    save,
    remove,
  } = useObjects({ available, confirm });

  if (!ready) return null;
  if (!available) {
    return (
      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-8">
        <EmptyState
          icon={FolderTree}
          title="Object store unavailable"
          body="Set ORCHESTRATOR_URL to connect this editor to a cluster."
        />
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Deployment + namespace pickers + refresh */}
      <div className="flex items-center gap-2 border-b border-black/10 px-4 py-2.5 dark:border-white/10">
        <Database size={15} className="shrink-0 text-zinc-400" />
        <select
          value={deploymentId ?? ""}
          onChange={(e) => selectDeployment(e.target.value)}
          className="min-w-0 max-w-md flex-1 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm dark:border-white/15"
        >
          <option value="">Select a deployment…</option>
          {sortedDeployments.map((d) => (
            <option key={d.id} value={d.id}>
              {deploymentLabel(d)}
            </option>
          ))}
        </select>
        {deploymentId && (
          <>
            <span className="shrink-0 text-xs font-medium text-zinc-400">
              namespace
            </span>
            <select
              value={namespace}
              onChange={(e) => selectNamespace(e.target.value)}
              aria-label="Namespace"
              className="shrink-0 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm dark:border-white/15"
            >
              {/* Show the loaded namespaces; until they load, the current one. */}
              {(namespaces ?? [namespace]).map((ns) => (
                <option key={ns} value={ns}>
                  {ns}
                </option>
              ))}
            </select>
            {secret && (
              <span
                className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-amber-600 dark:text-amber-400"
                title="Secret namespace: values are hidden; keys can be cleaned up only"
              >
                <Lock size={12} />
                read-only
              </span>
            )}
            <button
              type="button"
              onClick={reload}
              aria-label="Refresh objects"
              className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.05] hover:text-zinc-700 dark:hover:bg-white/[0.06] dark:hover:text-zinc-200"
            >
              <RefreshCw size={14} />
            </button>
          </>
        )}
      </div>

      {error && (
        <p className="mx-4 mt-3 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-500">
          {error}
        </p>
      )}

      {!deploymentId ? (
        <div className="px-6 py-8">
          <EmptyState
            icon={Database}
            title="Pick a deployment"
            body="Choose a deployment to browse the objects it holds, by namespace."
          />
        </div>
      ) : (
        <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[18rem_1fr]">
          {/* Key list */}
          <aside className="flex min-h-0 flex-col border-r border-black/10 dark:border-white/10">
            <div className="flex items-center justify-between px-3 py-2">
              <span className="text-xs font-semibold uppercase tracking-wide text-zinc-400">
                Keys{entries ? ` (${entries.length})` : ""}
              </span>
              {/* Creating requires writing a value, which secret namespaces forbid. */}
              {!secret && (
                <button
                  type="button"
                  onClick={startCreate}
                  className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-sky-600 transition-colors hover:bg-sky-500/10 dark:text-sky-400"
                >
                  <Plus size={13} />
                  New
                </button>
              )}
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
              {entries === null ? (
                <p className="px-2 py-2 text-sm text-zinc-400">Loading…</p>
              ) : entries.length === 0 ? (
                <p className="px-2 py-2 text-sm text-zinc-400">No objects yet.</p>
              ) : (
                entries.map((e) => {
                  const age = relativeAge(e.updatedAt);
                  return (
                    <button
                      key={e.key}
                      type="button"
                      onClick={() => selectKey(e.key)}
                      className={`flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left transition-colors ${
                        e.key === selectedKey
                          ? "bg-sky-500/10 text-sky-700 dark:text-sky-300"
                          : "hover:bg-black/[0.04] dark:hover:bg-white/[0.05]"
                      }`}
                    >
                      <span className="truncate font-mono text-xs">{e.key}</span>
                      <span className="text-[11px] text-zinc-400">
                        {e.size} B · v{e.version}
                        {age ? ` · ${age}` : ""}
                      </span>
                    </button>
                  );
                })
              )}
            </div>
          </aside>

        <ObjectEditor
          selectedKey={selectedKey}
          selectedEntry={selectedEntry}
          current={current}
          draft={draft}
          creating={creating}
          newKey={newKey}
          binary={binary}
          dirty={dirty}
          secret={secret}
          busy={busy}
          dispatch={dispatch}
          onSave={save}
          onRemove={remove}
        />
        </div>
      )}
    </div>
  );
}
