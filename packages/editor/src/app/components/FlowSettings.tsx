"use client";

import { Workflow } from "lucide-react";
import type { FlowDoc } from "../model/document";
import { duplicateNames, flowNames, slugify } from "../model/identity";
import { useEditorState, EditorActionType } from "../state/editorState";
import { CelScopeProvider } from "../scope/ScopeContext";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** Root-flow concurrency fields, with their runtime defaults for placeholder text. */
const TUNING_FIELDS: {
  field: "workers" | "buffer" | "pool";
  label: string;
  default: number;
  help: string;
}[] = [
  {
    field: "workers",
    label: "Workers",
    default: 8,
    help: "Concurrent message processors for this flow.",
  },
  {
    field: "buffer",
    label: "Buffer",
    default: 64,
    help: "Inbound message queue depth.",
  },
  {
    field: "pool",
    label: "Pool",
    default: 8,
    help: "Shared worker pool for parallel blocks (e.g. fork).",
  },
];

/**
 * Settings body for the active flow. Flows carry a name here (their source is
 * added and configured from the source node on the canvas, not this panel); root
 * flows also expose the runtime's concurrency tuning (workers/buffer/pool), which
 * sub-flows must not set.
 */
export default function FlowSettings({ flow }: { flow: FlowDoc }) {
  const { state, dispatch } = useEditorState();

  const duplicate =
    !!flow.name &&
    duplicateNames(flowNames(state.document)).has(flow.name);

  // Concurrency tuning applies to root flows only (the runtime rejects it on the
  // sub-flows nested in composite slots).
  const isRootFlow = state.document.flows.some((f) => f.id === flow.id);

  function setTuning(field: "workers" | "buffer" | "pool", raw: string) {
    const trimmed = raw.trim();
    const parsed = trimmed === "" ? undefined : Number(trimmed);
    // Ignore invalid / non-positive input; empty clears back to the default.
    const value =
      parsed === undefined
        ? undefined
        : Number.isFinite(parsed) && parsed >= 1
          ? Math.floor(parsed)
          : undefined;
    dispatch({
      type: EditorActionType.SET_FLOW_META,
      data: { flowId: flow.id, field, value },
    });
  }

  return (
    <>
      <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
        <Workflow size={18} className="text-zinc-500 shrink-0" />
        <span className="font-semibold tracking-tight truncate">Flow</span>
      </header>

      {/* Flow-level CEL is about the flow's result, so it sees the scope at its end. */}
      <CelScopeProvider site={{ kind: "flow-output", flowId: flow.id }}>
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

        {isRootFlow && (
          <div className="flex flex-col gap-4 border-t border-black/10 dark:border-white/10 pt-4">
            <p className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
              Concurrency
            </p>
            {TUNING_FIELDS.map(({ field, label, default: def, help }) => (
              <div key={field} className="flex flex-col gap-1">
                <label
                  htmlFor={`flow-${field}`}
                  className="text-xs font-medium text-zinc-600 dark:text-zinc-300"
                >
                  {label}
                </label>
                <input
                  id={`flow-${field}`}
                  type="number"
                  min={1}
                  step={1}
                  value={flow[field] ?? ""}
                  placeholder={`Default: ${def}`}
                  onChange={(e) => setTuning(field, e.target.value)}
                  className={INPUT}
                />
                <p className="text-xs text-zinc-400 dark:text-zinc-500">
                  {help} Default: {def}.
                </p>
              </div>
            ))}
          </div>
        )}
      </div>
      </CelScopeProvider>
    </>
  );
}
