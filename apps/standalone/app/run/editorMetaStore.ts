import type { EditorMetaStore } from "@octo/editor";
import { loadEditorMeta, saveEditorMeta } from "../actions/editorMeta";
import { unwrap } from "../actions/result";

/**
 * The editor-meta store: one `.octo/editor-meta.json` under the flows directory, keyed
 * by document internally, so one file describes them all and the integration id never
 * reaches the actions.
 *
 * Editing needs a saved document, since the meta is keyed by the flow file's name;
 * until then inputs live for the session.
 */
export const localEditorMetaStore: EditorMetaStore = {
  async load() {
    return unwrap(await loadEditorMeta());
  },
  async save(_integrationId, content) {
    unwrap(await saveEditorMeta(content));
  },
  canEdit(integrationId) {
    return !!integrationId;
  },
};
