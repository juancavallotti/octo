"use client";

import { Code2, LayoutGrid } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import type { FlowsView } from "../state/reducer";
import { BAR_VIEW_LABEL } from "./barButton";

/**
 * Canvas / YAML switch for the open file, beside its name in the document bar: both
 * are views of that one file, so the switch belongs with it rather than with the
 * header's project-wide tabs. Drives `state.flowsView`, which EditorBody reads.
 */
const OPTIONS: { view: FlowsView; label: string; icon: typeof LayoutGrid }[] = [
  { view: "canvas", label: "Canvas", icon: LayoutGrid },
  { view: "yaml", label: "YAML", icon: Code2 },
];

export default function FlowsViewToggle() {
  const { state, dispatch } = useEditorState();

  return (
    <div
      role="group"
      aria-label="File view"
      className="flex items-center gap-0.5 rounded-md border border-black/10 p-0.5 dark:border-white/15"
    >
      {OPTIONS.map(({ view, label, icon: Icon }) => {
        const active = state.flowsView === view;
        return (
          <button
            key={view}
            type="button"
            aria-pressed={active}
            title={label}
            onClick={() => dispatch({ type: EditorActionType.SET_FLOWS_VIEW, data: view })}
            className={`flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium transition-colors ${
              active
                ? "bg-black/10 text-zinc-900 dark:bg-white/15 dark:text-zinc-100"
                : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
            }`}
          >
            <Icon size={13} className="shrink-0" />
            <span className={BAR_VIEW_LABEL}>{label}</span>
          </button>
        );
      })}
    </div>
  );
}
