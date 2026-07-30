"use client";

import { Code2, FolderTree, LayoutGrid } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import { useResourceStore } from "../providers/ResourceStoreProvider";
import type { ViewMode } from "../state/reducer";

/**
 * Segmented Canvas / YAML / Resources switch for the editor body. It sits in the
 * app-owned top bar (next to the folder picker) and drives `state.viewMode`, which
 * EditorBody reads to swap the visual canvas, the read-only YAML preview, and the
 * Resources tab. The Resources option only appears when a resource store is
 * provided (the host backs it). The styling mirrors the LogPanel's tab buttons.
 */
const OPTIONS: { mode: ViewMode; label: string; icon: typeof LayoutGrid }[] = [
  { mode: "canvas", label: "Canvas", icon: LayoutGrid },
  { mode: "yaml", label: "YAML", icon: Code2 },
  { mode: "resources", label: "Resources", icon: FolderTree },
];

export default function ViewModeToggle() {
  const { state, dispatch } = useEditorState();
  const hasResources = useResourceStore() !== null;
  const options = hasResources
    ? OPTIONS
    : OPTIONS.filter((o) => o.mode !== "resources");

  return (
    <div
      role="group"
      aria-label="Editor view"
      className="flex items-center gap-0.5 rounded-md border border-black/10 dark:border-white/15 p-0.5"
    >
      {options.map(({ mode, label, icon: Icon }) => {
        const active = state.viewMode === mode;
        return (
          <button
            key={mode}
            type="button"
            aria-pressed={active}
            onClick={() =>
              dispatch({ type: EditorActionType.SET_VIEW_MODE, data: mode })
            }
            className={`flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium transition-colors ${
              active
                ? "bg-black/10 text-zinc-900 dark:bg-white/15 dark:text-zinc-100"
                : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
            }`}
          >
            <Icon size={14} className="shrink-0" />
            {label}
          </button>
        );
      })}
    </div>
  );
}
