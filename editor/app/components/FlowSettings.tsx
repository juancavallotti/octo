"use client";

import { Workflow } from "lucide-react";
import type { FlowDoc } from "@/app/model/document";
import { duplicateNames, flowNames, slugify } from "@/app/model/identity";
import { useEditorState, EditorActionType } from "@/app/state/editorState";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * Settings body for the active flow. Flows carry just a name here — their source
 * is added and configured from the source node on the canvas, not this panel.
 */
export default function FlowSettings({ flow }: { flow: FlowDoc }) {
  const { state, dispatch } = useEditorState();

  const duplicate =
    !!flow.name &&
    duplicateNames(flowNames(state.document)).has(flow.name);

  return (
    <>
      <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
        <Workflow size={18} className="text-zinc-500 shrink-0" />
        <span className="font-semibold tracking-tight truncate">Flow</span>
      </header>

      <div className="flex flex-col gap-4 overflow-y-auto p-4">
        <div className="flex flex-col gap-1">
          <label
            htmlFor="flow-name"
            className="text-xs font-medium text-zinc-600 dark:text-zinc-300"
          >
            Name
          </label>
          <input
            id="flow-name"
            type="text"
            value={flow.name}
            onChange={(e) =>
              dispatch({
                type: EditorActionType.RENAME_FLOW,
                data: { flowId: flow.id, name: slugify(e.target.value) },
              })
            }
            className={INPUT}
          />
          {duplicate ? (
            <p className="text-xs text-red-500">
              Another flow already uses this name.
            </p>
          ) : (
            <p className="text-xs text-zinc-400 dark:text-zinc-500">
              Referenced by name from flow-ref blocks.
            </p>
          )}
        </div>
      </div>
    </>
  );
}
