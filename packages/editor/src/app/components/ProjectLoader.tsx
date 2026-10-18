"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { RefreshCw, X } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import { projectFiles } from "../state/projectHandlers";
import { useFileSystem } from "../providers/FileSystemProvider";
import { parseProject, sameFiles, type ProjectFile } from "../model/project";

/**
 * Loads the project named by the editor's `projectId` via the filesystem capability,
 * and live-reloads it when it is written out from under us — the host bumps
 * `reloadToken` (e.g. from an @octo/events SSE stream) whenever an external write
 * (the MCP server) touches the open project. A clean editor reloads silently,
 * staying on the file it had open; if any file has unsaved local edits a dismissible
 * banner offers the reload rather than discarding them. Renders the banner (or
 * nothing); a no-op without an id or filesystem capability.
 */
export default function ProjectLoader({
  projectId,
  initialFile,
  reloadToken,
}: {
  projectId?: string;
  /** The file to open first; defaults to the first that holds a flow. */
  initialFile?: string;
  /** Bumped by the host to request a reload of the open project (external write). */
  reloadToken?: string | number;
}) {
  const { state, dispatch } = useEditorState();
  const fs = useFileSystem();
  // The files as last loaded from the store. The editor is "dirty" (has unsaved
  // edits) when any file now holds a different document, since every edit produces
  // a fresh document via the reducer.
  const loadedRef = useRef<ProjectFile[] | null>(null);
  // Monotonic id for in-flight loads, so a slow load that resolves after a newer
  // one (navigation, reload) — or after unmount — is ignored instead of clobbering.
  const loadSeq = useRef(0);
  const [pending, setPending] = useState(false);
  // The file to open: `initialFile` when a project is first loaded, then whichever
  // file is open, so a reload keeps the user where they were.
  const initialRef = useRef(initialFile);
  const activeRef = useRef(initialFile);
  useEffect(() => {
    initialRef.current = initialFile;
  }, [initialFile]);
  useEffect(() => {
    activeRef.current = state.activePath;
  }, [state.activePath]);

  const load = useCallback(() => {
    if (!projectId || !fs) return;
    const seq = ++loadSeq.current;
    fs.load(projectId)
      .then((stored) => {
        if (seq !== loadSeq.current) return; // superseded or unmounted
        const files = parseProject(stored.files);
        loadedRef.current = files;
        setPending(false);
        dispatch({
          type: EditorActionType.LOAD_PROJECT,
          data: {
            id: stored.id,
            name: stored.name,
            folderId: stored.folderId ?? null,
            files,
            activePath: activeRef.current,
          },
        });
      })
      .catch(() => {});
  }, [projectId, fs, dispatch]);

  // Initial load, and reload on navigation to a different id. The cleanup bumps
  // the sequence so an in-flight load can't apply after this effect tears down.
  useEffect(() => {
    activeRef.current = initialRef.current;
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
    if (!projectId || !fs) return;
    const dirty = loadedRef.current !== null && !sameFiles(loadedRef.current, projectFiles(state));
    if (dirty) setPending(true);
    else load();
  }, [reloadToken, projectId, fs, load, state]);

  if (!pending) return null;
  return (
    <div className="flex items-center justify-between gap-3 border-b border-amber-500/30 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
      <span className="flex items-center gap-2">
        <RefreshCw className="h-3.5 w-3.5 shrink-0" />
        This project was updated elsewhere. Reloading will discard your unsaved
        changes.
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
