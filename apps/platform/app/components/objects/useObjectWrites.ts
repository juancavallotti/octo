"use client";

import { useCallback } from "react";
import { deleteObject, setObject, type ObjectEntry, type ObjectValue } from "@/app/model/objects";

import type { Action } from "./state";

/**
 * The two writes the object browser makes, and the bookkeeping each needs.
 *
 * Both take the same shape — mark busy, write, re-read the list, tell the reducer
 * what happened, and mirror the new selection to the URL — and both report a
 * failure through the reducer rather than throwing, because the panel shows it
 * inline beside the value rather than replacing the view.
 *
 * The version each sends is the point of the whole thing: a write carries the
 * version it read, so a stale one surfaces the orchestrator's conflict instead of
 * silently overwriting whatever landed in between.
 */
export function useObjectWrites({
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
}: {
  deploymentId: string | null;
  namespace: string;
  selectedKey: string | null;
  selectedEntry: ObjectEntry | null;
  current: ObjectValue | null;
  draft: string;
  creating: boolean;
  newKey: string;
  secret: boolean;
  dispatch: (action: Action) => void;
  loadEntries: (deploymentId: string, namespace: string) => Promise<void>;
  writeUrl: (deploymentId: string | null, key: string | null, namespace: string) => void;
  confirm: (opts: {
    title: string;
    body?: string;
    confirmLabel?: string;
    danger?: boolean;
  }) => Promise<boolean>;
}) {
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
  }, [
    creating,
    current,
    deploymentId,
    dispatch,
    draft,
    loadEntries,
    namespace,
    newKey,
    writeUrl,
  ]);

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
    dispatch,
    loadEntries,
    namespace,
    secret,
    selectedEntry,
    selectedKey,
    writeUrl,
  ]);

  return { save, remove };
}
