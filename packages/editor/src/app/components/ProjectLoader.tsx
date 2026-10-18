"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { RefreshCw, X } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import { useFileSystem } from "../providers/FileSystemProvider";
import { fromDefinitionYaml } from "../model/runConfig";
import type { EditorDocument } from "../model/document";

/**
 * Loads the document named by the editor's `integrationId` via the filesystem
 * capability, and live-reloads it when the same file is written out from under us
 * — the host bumps `reloadToken` (e.g. from an @octo/events SSE stream) whenever
 * an external write (the MCP server) touches the open integration. A clean editor
 * reloads silently; if there are unsaved local edits a dismissible banner offers
 * the reload rather than discarding them. Renders the banner (or nothing); a
 * no-op without an id or filesystem capability.
 */
export default function IntegrationLoader({
  integrationId,
  reloadToken,
}: {
  integrationId?: string;
  /** Bumped by the host to request a reload of the open file (external write). */
  reloadToken?: string | number;
}) {
  const { state, dispatch } = useEditorState();
  const fs = useFileSystem();
  // The document reference as last loaded from the store. The editor is "dirty"
  // (has unsaved edits) when the current document is a different object, since
  // every edit produces a fresh document via the reducer.
  const loadedDocRef = useRef<EditorDocument | null>(null);
  // Monotonic id for in-flight loads, so a slow load that resolves after a newer
  // one (navigation, reload) — or after unmount — is ignored instead of clobbering.
  const loadSeq = useRef(0);
  const [pending, setPending] = useState(false);

  const load = useCallback(() => {
    if (!integrationId || !fs) return;
    const seq = ++loadSeq.current;
    fs.load(integrationId)
      .then((stored) => {
        if (seq !== loadSeq.current) return; // superseded or unmounted
        const document = fromDefinitionYaml(stored.definition);
        loadedDocRef.current = document;
        setPending(false);
        dispatch({
          type: EditorActionType.LOAD_INTEGRATION,
          data: {
            id: stored.id,
            name: stored.name,
            folderId: stored.folderId ?? null,
            document,
          },
        });
      })
      .catch(() => {});
  }, [integrationId, fs, dispatch]);

  // Initial load, and reload on navigation to a different id. The cleanup bumps
  // the sequence so an in-flight load can't apply after this effect tears down.
  useEffect(() => {
    load();
    return () => {
      loadSeq.current++;
    };
  }, [load]);

  // External-write reload: when the host bumps the token, reload silently if the
  // editor is clean, otherwise surface the banner so the user decides. The token's
  // previous value is tracked so unrelated re-renders don't retrigger.
  const seenToken = useRef(reloadToken);
  useEffect(() => {
    if (reloadToken === seenToken.current) return;
    seenToken.current = reloadToken;
    if (!integrationId || !fs) return;
    const dirty =
      loadedDocRef.current !== null && state.document !== loadedDocRef.current;
    if (dirty) setPending(true);
    else load();
  }, [reloadToken, integrationId, fs, load, state.document]);

  if (!pending) return null;
  return (
    <div className="flex items-center justify-between gap-3 border-b border-amber-500/30 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
      <span className="flex items-center gap-2">
        <RefreshCw className="h-3.5 w-3.5 shrink-0" />
        This integration was updated elsewhere. Reloading will discard your
        unsaved changes.
      </span>
      <span className="flex shrink-0 items-center gap-2">
        <button
          type="button"
          onClick={load}
          className="inline-flex items-center gap-1.5 rounded-md bg-amber-600 px-3 py-1 text-sm font-medium text-white hover:bg-amber-500"
        >
          <RefreshCw className="h-3.5 w-3.5" />
          Reload
        </button>
        <button
          type="button"
          onClick={() => setPending(false)}
          title="Dismiss"
          className="inline-flex items-center rounded-md p-1 text-amber-900/70 hover:bg-amber-500/15 dark:text-amber-200/70"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </span>
    </div>
  );
}
