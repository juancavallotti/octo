"use client";

import { Plus, X } from "lucide-react";
import type { BlockNode, FlowDoc } from "@/app/model/document";
import type { FieldSpec } from "@/app/schema/types";
import { useEditorState, EditorActionType } from "@/app/state/editorState";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** The singular noun and per-row label for a list slot ("case" vs "branch"). */
function nounFor(field: FieldSpec): { noun: string; label: (i: number) => string } {
  switch (field.type) {
    case "case-list":
      return { noun: "case", label: (i) => `Case ${i + 1}` };
    case "route-list":
      return { noun: "route", label: (i) => `Route ${i + 1}` };
    case "tool-list":
      return { noun: "tool", label: (i) => `Tool ${i + 1}` };
    default:
      return { noun: "branch", label: (i) => `Branch ${i + 1}` };
  }
}

/**
 * Manages a composite block's list slot (switch `cases`, fork `branches`,
 * ai-router `routes`, ai-agent `tools`) from the properties panel: add a path with
 * the "+" button (it renders a new SubFlow on the canvas), edit each path's
 * per-entry metadata (a case's CEL `when`; a route/tool's name + description; a
 * tool's input JSON Schema), and remove paths. The steps inside each path are
 * still edited on the canvas.
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
  const named = field.type === "route-list" || field.type === "tool-list";

  const setName = (flow: FlowDoc, name: string) =>
    dispatch({
      type: EditorActionType.RENAME_FLOW,
      data: { flowId: flow.id, name },
    });
  const setMeta = (
    flow: FlowDoc,
    metaField: "when" | "description" | "inputSchema",
    value: string,
  ) =>
    dispatch({
      type: EditorActionType.SET_FLOW_META,
      data: { flowId: flow.id, field: metaField, value },
    });

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
          {field.type === "case-list" && (
            <textarea
              rows={2}
              value={flow.when ?? ""}
              placeholder="CEL expression — when to take this case"
              onChange={(e) => setMeta(flow, "when", e.target.value)}
              className={`${INPUT} resize-y font-mono`}
            />
          )}
          {named && (
            <>
              <input
                type="text"
                aria-label={`${noun} name`}
                value={flow.name ?? ""}
                placeholder={`${noun} name`}
                onChange={(e) => setName(flow, e.target.value)}
                className={INPUT}
              />
              <textarea
                rows={2}
                aria-label={`${noun} description`}
                value={flow.description ?? ""}
                placeholder="Description the model uses to choose this path"
                onChange={(e) => setMeta(flow, "description", e.target.value)}
                className={`${INPUT} resize-y`}
              />
            </>
          )}
          {field.type === "tool-list" && (
            <textarea
              rows={3}
              aria-label="tool input schema"
              value={flow.inputSchema ?? ""}
              placeholder="Input JSON Schema (optional)"
              onChange={(e) => setMeta(flow, "inputSchema", e.target.value)}
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
