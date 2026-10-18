"use client";

import { useEditorState, EditorActionType } from "../state/editorState";
import { useSave } from "../save/SaveContext";

/**
 * The Google-docs-style editable integration title in the header. Borderless
 * until hovered/focused so it reads as a heading, not a form field. Unlike flow
 * and connection names this is a human display name, so spaces are preserved
 * (no slugify). Enter commits the rename by saving (and blurs the field), the
 * standard rename UX; saving is a no-op when there is nothing to persist.
 */
export default function IntegrationTitle() {
  const { state, dispatch } = useEditorState();
  const save = useSave();

  return (
    <input
      type="text"
      value={state.project.name}
      placeholder="Untitled integration"
      aria-label="Integration title"
      onChange={(e) =>
        dispatch({
          type: EditorActionType.SET_PROJECT_TITLE,
          data: { name: e.target.value },
        })
      }
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          e.currentTarget.blur();
          void save?.save();
        }
      }}
      className="max-w-[16rem] rounded-md border border-transparent bg-transparent px-2 py-1 text-sm font-medium text-zinc-800 transition-colors hover:border-black/10 focus:border-black/20 focus:outline-none dark:text-zinc-100 dark:hover:border-white/15 dark:focus:border-white/25"
    />
  );
}
