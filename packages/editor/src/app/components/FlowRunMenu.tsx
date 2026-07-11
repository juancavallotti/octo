"use client";

import { useState } from "react";
import { Pencil, Play, Plus, Trash2 } from "lucide-react";
import { Popover } from "../../components/ui";
import { useRun } from "../run/RunContext";
import { useFlowRun } from "../run/FlowRunContext";
import { useEditorMeta } from "../providers/EditorMetaProvider";
import { issueMessages } from "../model/validate";
import type { TestInput } from "../meta/types";
import TestInputForm from "./TestInputForm";

/**
 * The play button on a flow card, and the menu it opens: the flow's saved test inputs,
 * one click each to run the flow with it.
 *
 * A flow can always be run with no input (an empty message) — the commonest case for a
 * flow whose source supplies nothing anyway — so that is the first entry rather than
 * something the user has to construct.
 *
 * Renders nothing without a run capability. When the document is invalid, the button is
 * disabled and says why: the runner would only reject it.
 */
export default function FlowRunMenu({ flowId }: { flowId: string }) {
  const run = useRun();
  const flowRun = useFlowRun();
  const meta = useEditorMeta();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<TestInput | "new" | null>(null);

  if (!run?.available || !flowRun) return null;

  const inputs = meta?.inputs(flowId) ?? [];
  const blocked = !run.validation.ok;
  const title = blocked
    ? `Fix before running:\n• ${issueMessages(run.validation).join("\n• ")}`
    : "Run this flow";

  const start = (input?: TestInput) => {
    setOpen(false);
    void flowRun.runFlow(flowId, input);
  };

  const close = () => {
    setOpen(false);
    setEditing(null);
  };

  return (
    <Popover
      open={open}
      onClose={close}
      panelClassName="left-0 w-72"
      trigger={
        <button
          type="button"
          aria-label="Run flow"
          title={title}
          disabled={blocked}
          onClick={(e) => {
            e.stopPropagation();
            setOpen((o) => !o);
          }}
          // Unlike the delete X, this is not hidden until hover: running a flow is the
          // thing you came here to do, so it should be visible enough to find. It still
          // brightens on hover, and dims out when the document can't run.
          className="rounded-full p-0.5 text-zinc-400 opacity-70 transition hover:text-emerald-600 hover:opacity-100 disabled:cursor-not-allowed disabled:opacity-30 disabled:hover:text-zinc-400 group-hover:opacity-100 dark:hover:text-emerald-400"
        >
          <Play size={14} />
        </button>
      }
    >
      <div onClick={(e) => e.stopPropagation()}>
        {editing ? (
          <TestInputForm
            initial={editing === "new" ? undefined : editing}
            onCancel={() => setEditing(null)}
            onSubmit={(input) => {
              if (editing === "new") meta?.addInput(flowId, input);
              else meta?.updateInput(flowId, { ...editing, ...input });
              setEditing(null);
            }}
          />
        ) : (
          <div className="flex flex-col">
            <div className="border-b border-black/10 px-3 py-2 text-sm font-medium dark:border-white/10">
              Run with…
            </div>

            <ul className="max-h-[50vh] overflow-y-auto py-1">
              <li>
                <button
                  type="button"
                  onClick={() => start()}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                >
                  <Play size={13} className="shrink-0 text-zinc-400" />
                  No input
                  <span className="ml-auto text-xs text-zinc-400">empty message</span>
                </button>
              </li>

              {inputs.map((input) => (
                <li key={input.id} className="group/row flex items-center">
                  <button
                    type="button"
                    onClick={() => start(input)}
                    className="flex flex-1 items-center gap-2 truncate px-3 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                  >
                    <Play size={13} className="shrink-0 text-zinc-400" />
                    <span className="truncate">{input.name}</span>
                  </button>
                  <button
                    type="button"
                    aria-label={`Edit ${input.name}`}
                    onClick={() => setEditing(input)}
                    className="rounded p-1 text-zinc-400 opacity-0 hover:text-zinc-700 group-hover/row:opacity-100 dark:hover:text-zinc-200"
                  >
                    <Pencil size={13} />
                  </button>
                  <button
                    type="button"
                    aria-label={`Delete ${input.name}`}
                    onClick={() => meta?.removeInput(flowId, input.id)}
                    className="mr-2 rounded p-1 text-zinc-400 opacity-0 hover:text-red-500 group-hover/row:opacity-100"
                  >
                    <Trash2 size={13} />
                  </button>
                </li>
              ))}
            </ul>

            <div className="border-t border-black/10 dark:border-white/10">
              <button
                type="button"
                onClick={() => setEditing("new")}
                className="flex w-full items-center gap-1.5 px-3 py-2 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
              >
                <Plus size={14} />
                Add test input
              </button>
              {meta && !meta.canPersist && (
                <p className="px-3 pb-2 text-xs text-zinc-400 dark:text-zinc-500">
                  Save the flow to keep its test inputs.
                </p>
              )}
            </div>
          </div>
        )}
      </div>
    </Popover>
  );
}
