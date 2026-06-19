"use client";

import { Plus, X } from "lucide-react";
import type { BlockNode } from "@/app/model/document";
import type { FieldSpec } from "@/app/schema/types";
import { useEditorState, EditorActionType } from "@/app/state/editorState";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** The singular noun and per-row label for a list slot ("case" vs "branch"). */
function nounFor(field: FieldSpec): { noun: string; label: (i: number) => string } {
  if (field.type === "case-list") {
    return { noun: "case", label: (i) => `Case ${i + 1}` };
  }
  return { noun: "branch", label: (i) => `Branch ${i + 1}` };
}

/**
 * Manages a composite block's list slot (switch `cases` / fork `branches`) from
 * the properties panel: add a path with the "+" button (it renders a new SubFlow
 * on the canvas), edit each case's CEL `when` guard, and remove paths. The steps
 * inside each path are still edited on the canvas.
 */
export default function SlotListEditor({
  block,
  field,
}: {
  block: BlockNode;
  field: FieldSpec;
}) {
  const { dispatch } = useEditorState();
  const entries = block.slots?.[field.name] ?? [];
  const { noun, label } = nounFor(field);
  const isCase = field.type === "case-list";

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-medium text-zinc-600 dark:text-zinc-300">
        {field.label}
      </span>

      {entries.map((flow, i) => (
        <div
          key={flow.id}
          className="flex flex-col gap-1.5 rounded-lg border border-black/[0.06] p-2 dark:border-white/[0.06]"
        >
          <div className="flex items-center gap-2">
            <span className="font-mono text-[11px] text-zinc-500">
              {label(i)}
            </span>
            <button
              type="button"
              aria-label={`Remove ${label(i)}`}
              onClick={() =>
                dispatch({
                  type: EditorActionType.REMOVE_SLOT_FLOW,
                  data: { blockId: block.id, field: field.name, flowId: flow.id },
                })
              }
              className="ml-auto shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
            >
              <X size={14} />
            </button>
          </div>
          {isCase && (
            <textarea
              rows={2}
              value={flow.when ?? ""}
              placeholder="CEL expression — when to take this case"
              onChange={(e) =>
                dispatch({
                  type: EditorActionType.SET_FLOW_WHEN,
                  data: { flowId: flow.id, when: e.target.value },
                })
              }
              className={`${INPUT} resize-y font-mono`}
            />
          )}
        </div>
      ))}

      <button
        type="button"
        onClick={() =>
          dispatch({
            type: EditorActionType.ADD_SLOT_FLOW,
            data: { blockId: block.id, field: field.name },
          })
        }
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add {noun}
      </button>
    </div>
  );
}
