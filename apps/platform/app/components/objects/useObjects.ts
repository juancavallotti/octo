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
  listAllDeployments,
  type DeploymentWithIntegration,
} from "@/app/model/orchestrator";
import { getObject, listNamespaces, listObjects } from "@/app/model/objects";
import {
  DEFAULT_NAMESPACE,
  initState,
  isSecretNamespace,
  reducer,
} from "./state";
import { deploymentLabel } from "./format";
import { useObjectWrites } from "./useObjectWrites";

/**
 * The object browser's data lifecycle: which deployment and key are selected
 * (mirrored to the URL so an object is bookmarkable), the three lists that follow
 * from that selection, and the read/write/delete of a value.
 *
 * The interlinked selection and editing state stays in the reducer next door;
 * this owns the fetching, the URL sync, and the ordering of the two against each
 * other. What is left in ObjectsManager is rendering.
 *
 * The race guard is the part worth knowing about: selecting a key starts a fetch,
 * and selecting another before it lands must not let the first answer overwrite
 * the second. Each fetch takes a sequence number and drops its own result if a
 * newer one has been issued.
 */
export function useObjects({
  available,
  confirm,
}: {
  /** The orchestrator probe resolved and it is there; nothing loads until it has. */
  available: boolean;
  confirm: (opts: {
    title: string;
    body?: string;
    confirmLabel?: string;
    danger?: boolean;
  }) => Promise<boolean>;
}) {
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

  const { save, remove } = useObjectWrites({
    deploymentId,
    namespace,
    selectedKey,
    selectedEntry,
    current,
    draft,
    creating,
    newKey,
    secret,
    dispatch,
    loadEntries,
    writeUrl,
    confirm,
  });

  const sortedDeployments = useMemo(
    () =>
      [...(deployments ?? [])].sort((a, b) =>
        deploymentLabel(a).localeCompare(deploymentLabel(b)),
      ),
    [deployments],
  );


  return {
    // Selection, mirrored to the URL.
    deploymentId,
    namespace,
    selectedKey,
    // The three lists that follow from it.
    sortedDeployments,
    namespaces,
    entries,
    selectedEntry,
    // The value being viewed or written.
    current,
    draft,
    creating,
    newKey,
    binary,
    dirty,
    secret,
    // Status.
    busy,
    error,
    dispatch,
    // Actions.
    /** Re-read the current key list; the refresh button's handler. */
    reload: () => {
      if (deploymentId) loadEntries(deploymentId, namespace);
    },
    selectDeployment,
    selectNamespace,
    selectKey,
    startCreate,
    save,
    remove,
  };
}
