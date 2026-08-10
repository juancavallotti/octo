"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  Database,
  FolderTree,
  Lock,
  Plus,
  RefreshCw,
  Save,
  Trash2,
} from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import {
  listAllDeployments,
  type DeploymentWithIntegration,
} from "@/app/model/orchestrator";
import {
  deleteObject,
  getObject,
  listNamespaces,
  listObjects,
  setObject,
} from "@/app/model/objects";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import {
  DEFAULT_NAMESPACE,
  initState,
  isSecretNamespace,
  reducer,
} from "./state";
import { relativeAge } from "@/app/lib/relativeAge";

/** A readable label for a deployment in the picker. */
function deploymentLabel(d: DeploymentWithIntegration): string {
  return `${d.integrationName}${d.tag ? ` · ${d.tag}` : ""} (${d.status})`;
}

/**
 * The object store browser (`/platform/objects`): pick a deployment, list the keys
 * it holds in the user-facing object namespace, and view / edit / create / delete
 * their values. The selected deployment and key are mirrored to the URL so a
 * specific object is bookmarkable. Writes use the stored version for optimistic
 * concurrency; a stale write surfaces the orchestrator's conflict. The interlinked
 * selection/editing state lives in a reducer (./state); this component owns the
 * fetching and URL sync.
 */
export default function ObjectsManager() {
  const { available, ready } = useOrchestrator();
  const confirm = useConfirm();
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const [deployments, setDeployments] = useState<
    DeploymentWithIntegration[] | null
  >(null);
  const [state, dispatch] = useReducer(reducer, undefined, () =>
    initState(
      searchParams.get("deployment"),
      searchParams.get("key"),
      searchParams.get("ns"),
    ),
  );
  const {
    deploymentId,
    namespaces,
    namespace,
    entries,
    selectedKey,
    current,
    draft,
    creating,
    newKey,
    busy,
    error,
  } = state;

  // Guards a slower value fetch from overwriting a newer selection.
  const valueSeq = useRef(0);

  /** Mirror the current selection into the URL (bookmarkable, no navigation). The
   *  default namespace is encoded by omitting the param, to keep URLs clean. */
  const writeUrl = useCallback(
    (dep: string | null, key: string | null, ns: string) => {
      const p = new URLSearchParams();
      if (dep) p.set("deployment", dep);
      if (ns && ns !== DEFAULT_NAMESPACE) p.set("ns", ns);
      if (key) p.set("key", key);
      const qs = p.toString();
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [pathname, router],
  );

  // Load the deployment list once the orchestrator is reachable.
  useEffect(() => {
    if (!available) return;
    listAllDeployments().then(
      (ds) => setDeployments(ds),
      (e) => dispatch({ type: "error", error: (e as Error).message }),
    );
  }, [available]);

  // Load the deployment's namespaces whenever the selected deployment changes.
  useEffect(() => {
    if (!available || !deploymentId) return;
    listNamespaces(deploymentId).then(
      (ns) => dispatch({ type: "namespacesLoaded", namespaces: ns }),
      () => dispatch({ type: "namespacesLoaded", namespaces: [DEFAULT_NAMESPACE] }),
    );
  }, [available, deploymentId]);

  const loadEntries = useCallback(
    (dep: string, ns: string) =>
      listObjects(dep, ns).then(
        (items) => dispatch({ type: "entriesLoaded", entries: items }),
        (e) => {
          dispatch({ type: "entriesLoaded", entries: [] });
          dispatch({ type: "error", error: (e as Error).message });
        },
      ),
    [],
  );

  // (Re)load the key list whenever the selected deployment or namespace changes.
  useEffect(() => {
    if (!available || !deploymentId) return;
    loadEntries(deploymentId, namespace);
  }, [available, deploymentId, namespace, loadEntries]);

  // Load the selected key's value (and version), guarding against races. Secret
  // namespaces never load a value — the panel offers cleanup (delete) only.
  useEffect(() => {
    if (!deploymentId || !selectedKey || isSecretNamespace(namespace)) return;
    const seq = ++valueSeq.current;
    getObject(deploymentId, selectedKey, namespace).then(
      (v) => {
        if (seq === valueSeq.current) dispatch({ type: "valueLoaded", value: v });
      },
      (e) => {
        if (seq === valueSeq.current)
          dispatch({ type: "error", error: (e as Error).message });
      },
    );
  }, [deploymentId, namespace, selectedKey]);

  const selectDeployment = useCallback(
    (dep: string) => {
      dispatch({ type: "selectDeployment", deploymentId: dep || null });
      writeUrl(dep || null, null, DEFAULT_NAMESPACE);
    },
    [writeUrl],
  );

  const selectNamespace = useCallback(
    (ns: string) => {
      dispatch({ type: "selectNamespace", namespace: ns });
      writeUrl(deploymentId, null, ns);
    },
    [deploymentId, writeUrl],
  );

  const selectKey = useCallback(
    (key: string) => {
      dispatch({ type: "selectKey", key });
      writeUrl(deploymentId, key, namespace);
    },
    [deploymentId, namespace, writeUrl],
  );

  const startCreate = useCallback(() => {
    dispatch({ type: "startCreate" });
    writeUrl(deploymentId, null, namespace);
  }, [deploymentId, namespace, writeUrl]);

  // The value is binary (returned base64); show it read-only rather than risk a
  // lossy text edit.
  const binary = current?.encoding === "base64";
  const dirty = current != null && !binary && draft !== current.value;
  // Secret namespaces are browse + cleanup only: list keys, delete them, but never
  // view or edit a value.
  const secret = isSecretNamespace(namespace);
  const selectedEntry =
    entries?.find((e) => e.key === selectedKey) ?? null;

  const save = useCallback(async () => {
    if (!deploymentId) return;
    dispatch({ type: "busy" });
    try {
      if (creating) {
        const key = newKey.trim();
        if (!key) return dispatch({ type: "cancelCreate" });
        await setObject(deploymentId, key, draft, 0, "utf8", namespace);
        await loadEntries(deploymentId, namespace);
        dispatch({ type: "created", key });
        writeUrl(deploymentId, key, namespace);
      } else if (current) {
        const version = await setObject(
          deploymentId,
          current.key,
          draft,
          current.version,
          "utf8",
          namespace,
        );
        dispatch({ type: "saved", current: { ...current, value: draft, version } });
        await loadEntries(deploymentId, namespace);
      }
    } catch (e) {
      dispatch({ type: "error", error: (e as Error).message });
    }
  }, [creating, current, deploymentId, draft, loadEntries, namespace, newKey, writeUrl]);

  const remove = useCallback(async () => {
    // For a viewable object the version comes from the loaded value; for a secret
    // key (no value loaded) it comes from the list entry.
    const key = current?.key ?? selectedKey;
    if (!deploymentId || !key) return;
    const version = current?.version ?? selectedEntry?.version ?? 0;
    const ok = await confirm({
      title: `Delete "${key}"?`,
      body: secret
        ? "This permanently removes the secret from this deployment (e.g. to clear stale credentials)."
        : "This permanently removes the object from this deployment.",
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    dispatch({ type: "busy" });
    try {
      await deleteObject(deploymentId, key, version, namespace);
      await loadEntries(deploymentId, namespace);
      dispatch({ type: "deleted" });
      writeUrl(deploymentId, null, namespace);
    } catch (e) {
      dispatch({ type: "error", error: (e as Error).message });
    }
  }, [
    confirm,
    current,
    deploymentId,
    loadEntries,
    namespace,
    secret,
    selectedEntry,
    selectedKey,
    writeUrl,
  ]);

  const sortedDeployments = useMemo(
    () =>
      [...(deployments ?? [])].sort((a, b) =>
        deploymentLabel(a).localeCompare(deploymentLabel(b)),
      ),
    [deployments],
  );

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
              onClick={() => loadEntries(deploymentId, namespace)}
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

          {/* Detail / editor */}
          <section className="flex min-h-0 flex-col overflow-y-auto">
            {creating ? (
              <div className="flex min-h-0 flex-1 flex-col gap-3 p-4">
                <input
                  autoFocus
                  value={newKey}
                  onChange={(e) => dispatch({ type: "setNewKey", value: e.target.value })}
                  placeholder="key (may contain slashes)"
                  className="rounded-md border border-black/10 bg-transparent px-2.5 py-1.5 font-mono text-sm dark:border-white/15"
                />
                <textarea
                  value={draft}
                  onChange={(e) => dispatch({ type: "setDraft", value: e.target.value })}
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
                    onClick={save}
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
                    onClick={remove}
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
                    onClick={remove}
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
                  onChange={(e) => dispatch({ type: "setDraft", value: e.target.value })}
                  readOnly={binary}
                  spellCheck={false}
                  className="min-h-0 flex-1 resize-none rounded-md border border-black/10 bg-transparent p-3 font-mono text-sm read-only:opacity-70 dark:border-white/15"
                />

                <div className="flex items-center justify-end gap-2">
                  <button
                    type="button"
                    onClick={save}
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
        </div>
      )}
    </div>
  );
}
