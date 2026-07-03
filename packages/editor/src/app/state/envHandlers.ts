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

export function setResources(
  state: EditorState,
  p: SetResourcesPayload,
): EditorState {
  return { ...state, document: { ...state.document, resources: p.resources } };
}
