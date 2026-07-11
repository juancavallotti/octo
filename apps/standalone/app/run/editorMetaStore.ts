import type { EditorMetaStore } from "@octo/editor";
import { loadEditorMeta, saveEditorMeta } from "../actions/editorMeta";
import { unwrap } from "../actions/result";

/**
 * The standalone editor-meta store: one `.octo/editor-meta.json` under the flows
 * directory, shared by every flow there (the same way `.env.dev` is). The file is keyed
 * by document internally, so one file describes the whole directory — which is why the
 * integration id is not passed to the actions.
 *
 * Editing needs a saved document: the meta is keyed by the flow file's name, and an
 * unsaved draft does not have one yet. Until then inputs live for the session.
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
