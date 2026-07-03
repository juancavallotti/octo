"use client";

import { useMemo } from "react";
import Prism from "prismjs";
import "prismjs/components/prism-yaml";
import { useEditorState } from "../state/editorState";
import { toDefinitionYaml } from "../model/runConfig";

/**
 * Read-only YAML preview of the current document — the definition the editor
 * would save (see {@link toDefinitionYaml}). It replaces the canvas when the view
 * mode is "yaml" (driven from the header's ViewModeToggle) and re-derives on every
 * document change, so it stays in sync as the user edits. Highlighting is done
 * synchronously by Prism; token colors are themed in editor.css.
 */
export default function YamlPreview() {
  const { state } = useEditorState();

  const html = useMemo(() => {
    const yaml = toDefinitionYaml(state.document, state.integration.name);
    // Overlay the handlebars token so `{{ }}` template expressions stand out here
    // too, consistent with the Resources editors.
    const grammar = {
      handlebars: { pattern: /\{\{[\s\S]*?\}\}/, greedy: true },
      ...Prism.languages.yaml,
    };
    return Prism.highlight(yaml, grammar, "yaml");
  }, [state.document, state.integration.name]);

  return (
    <div className="flex-1 min-h-0 overflow-auto bg-zinc-50 dark:bg-zinc-900">
      <pre className="octo-yaml-preview m-0 p-4 text-xs leading-relaxed font-mono text-zinc-800 dark:text-zinc-200">
        <code dangerouslySetInnerHTML={{ __html: html }} />
      </pre>
    </div>
  );
}
