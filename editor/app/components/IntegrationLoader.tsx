"use client";

import { useEffect } from "react";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import {
  findIntegrationFolderId,
  getIntegration,
} from "@/app/model/orchestrator";
import { fromDefinitionYaml } from "@/app/model/runConfig";

/**
 * Loads the integration named by the `/i/[id]` route into the editor. The id
 * lives in the path (not a query string) so the URL is bookmarkable and survives
 * a refresh. The integration's folder is resolved from membership since the
 * integration record itself doesn't carry it. Renders nothing.
 */
export default function IntegrationLoader({
  integrationId,
}: {
  integrationId?: string;
}) {
  const { dispatch } = useEditorState();

  useEffect(() => {
    if (!integrationId) return;
    let cancelled = false;
    Promise.all([
      getIntegration(integrationId),
      findIntegrationFolderId(integrationId),
    ])
      .then(([integration, folderId]) => {
        if (cancelled) return;
        dispatch({
          type: EditorActionType.LOAD_INTEGRATION,
          data: {
            id: integration.id,
            name: integration.name,
            folderId,
            document: fromDefinitionYaml(integration.definition),
          },
        });
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [integrationId, dispatch]);

  return null;
}
