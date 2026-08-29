"use client";

import { Database, FolderTree, Plus } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import { relativeAge } from "@/app/lib/relativeAge";
import { useObjects } from "./useObjects";
import ObjectEditor from "./ObjectEditor";
import ObjectsToolbar from "./ObjectsToolbar";

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
      <ObjectsToolbar
        deployments={sortedDeployments}
        deploymentId={deploymentId}
        onSelectDeployment={selectDeployment}
        namespaces={namespaces}
        namespace={namespace}
        onSelectNamespace={selectNamespace}
        secret={secret}
        onRefresh={reload}
      />

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
