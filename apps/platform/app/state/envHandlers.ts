import type { EditorState } from "./reducer";
import type { SetEnvPayload } from "./actions";

/**
 * Pure state transitions for the document's declared environment variables (the
 * runtime's top-level `env:`). Kept apart from handlers.ts, like sourceHandlers and
 * connectionHandlers, to keep each file small and focused.
 */

export function setEnv(state: EditorState, p: SetEnvPayload): EditorState {
  return { ...state, document: { ...state.document, env: p.env } };
}
