import type { EditorState } from "./reducer";
import type { SetEnvPayload, SetResourcesPayload } from "./actions";

/**
 * Pure state transitions for the document's declared environment variables (the
 * runtime's top-level `env:`) and resources (`resources:`). Kept apart from
 * handlers.ts, like sourceHandlers and connectionHandlers, to keep each file small
 * and focused.
 */

export function setEnv(state: EditorState, p: SetEnvPayload): EditorState {
  return { ...state, document: { ...state.document, env: p.env } };
}

/** Replace the `resources:` block of each file named, open or not. */
export function setResources(
  state: EditorState,
  p: SetResourcesPayload,
): EditorState {
  const next = new Map(p.changes.map((c) => [c.path, c.resources]));
  const active = next.get(state.activePath);
  return {
    ...state,
    document: active ? { ...state.document, resources: active } : state.document,
    // The open file's entry is stale by design (its present is `document`), so it is
    // left alone; projectFiles() reads the open file from `document`.
    files: state.files.map((f) => {
      const resources = f.path === state.activePath ? undefined : next.get(f.path);
      return resources ? { ...f, document: { ...f.document, resources } } : f;
    }),
  };
}
