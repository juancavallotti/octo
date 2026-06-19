"use client";

import { useEffect } from "react";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import { getIntegration } from "@/app/model/orchestrator";
import { fromDefinitionYaml } from "@/app/model/runConfig";

/**
 * Bridges the management route to the editor: when the page is opened with
 * `?integration=<id>` (optionally `&folder=<id>`) it loads that integration into
 * the editor; with `?new=1` it starts a fresh draft. The query string is then
 * stripped so a later refresh won't clobber in-progress edits. Renders nothing.
 *
 * Reads `window.location` rather than `useSearchParams` to avoid forcing a
 * Suspense boundary on this otherwise-static route.
 */
export default function IntegrationLoader() {
  const { dispatch } = useEditorState();

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const id = params.get("integration");
    const isNew = params.get("new");
    if (!id && !isNew) return;

    const clearQuery = () =>
      window.history.replaceState(null, "", window.location.pathname);

    if (isNew) {
      dispatch({ type: EditorActionType.NEW_INTEGRATION });
      clearQuery();
      return;
    }

    let cancelled = false;
    getIntegration(id as string)
      .then((integration) => {
        if (cancelled) return;
        dispatch({
          type: EditorActionType.LOAD_INTEGRATION,
          data: {
            id: integration.id,
            name: integration.name,
            folderId: params.get("folder"),
            document: fromDefinitionYaml(integration.definition),
          },
        });
        clearQuery();
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [dispatch]);

  return null;
}
