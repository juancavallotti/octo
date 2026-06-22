"use client";

import { useEffect } from "react";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import { fromDefinitionYaml } from "@/app/model/runConfig";

/**
 * Dev-only counterpart to IntegrationLoader: loads a repo sample (by slug) into
 * the editor from `/api/preview-sample`, so `/preview?sample=<name>` renders the
 * flow on the canvas with no orchestrator. Used by the Playwright screenshot
 * harness. Renders nothing.
 */
export default function PreviewLoader({ sample }: { sample?: string }) {
  const { dispatch } = useEditorState();

  useEffect(() => {
    if (!sample) return;
    let cancelled = false;
    fetch(`/api/preview-sample?name=${encodeURIComponent(sample)}`)
      .then((res) => {
        if (!res.ok) throw new Error(`sample ${sample}: ${res.status}`);
        return res.text();
      })
      .then((yaml) => {
        if (cancelled) return;
        dispatch({
          type: EditorActionType.LOAD_DOCUMENT,
          data: { document: fromDefinitionYaml(yaml) },
        });
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [sample, dispatch]);

  return null;
}
