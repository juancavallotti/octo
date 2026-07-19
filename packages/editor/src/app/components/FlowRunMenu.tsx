"use client";

import { useState } from "react";
import { Clock, Pencil, Play, Plus, Trash2 } from "lucide-react";
import { Popover } from "../../components/ui";
import { useRun } from "../run/RunContext";
import { useFlowRun } from "../run/FlowRunContext";
import { useEditorState } from "../state/editorState";
import { useEditorMeta } from "../providers/EditorMetaProvider";
import { issueMessages } from "../model/validate";
import type { SourceNode } from "../model/document";
import type { TestInput } from "../meta/types";
import TestInputForm from "./TestInputForm";

/**
 * The CEL payload a cron source declares, or null for any other source. A cron source's
 * `payload` *is* the body the flow receives on every tick, so we can offer it as a
 * ready-made test input rather than making the user retype what the document already says.
 * Other sources are only partially derivable, so they are out of scope (see #143).
 */
function cronPayloadOf(source: SourceNode | undefined): string | null {
  if (!source || (source.connector !== "cron" && source.type !== "cron")) return null;
  const payload = source.settings?.payload;
  return typeof payload === "string" && payload.trim() !== "" ? payload : null;
}

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
  const { state } = useEditorState();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<TestInput | "new" | null>(null);
  const [deriving, setDeriving] = useState(false);
  const [deriveError, setDeriveError] = useState<string | null>(null);

  if (!run?.available || !flowRun) return null;

  const inputs = meta?.inputs(flowId) ?? [];
  const cronPayload = cronPayloadOf(state.document.flows.find((f) => f.id === flowId)?.source);
  const blocked = !run.validation.ok;
  const title = blocked
    ? `Fix before running:\n• ${issueMessages(run.validation).join("\n• ")}`
    : "Run this flow";

  const start = (input?: TestInput) => {
    setOpen(false);
    void flowRun.runFlow(flowId, input);
  };

  // Run the flow with the body its cron source would produce. The payload is a CEL
  // expression (it may read `now`, the fire time), so we evaluate it to a concrete body
  // through the same evalCel the CEL tester uses — no runtime change, and the derived
  // input is never persisted: it is computed from the document, so saving it would just
  // create a stale copy. An expression that fails to evaluate stays on screen as an error
  // rather than running the flow with a body nobody meant.
  const startFromCron = async () => {
    if (!cronPayload) return;
    setDeriving(true);
    setDeriveError(null);
    try {
      const res = await run.evalCel({ expression: cronPayload });
      if (!res.ok || res.error !== undefined) {
        setDeriveError(res.error ?? "the cron payload did not evaluate");
        return;
      }
      start({
        id: "cron-derived",
        name: "As the cron source would send it",
        data: JSON.stringify(res.result ?? null),
      });
    } catch (err) {
      setDeriveError((err as Error).message);
    } finally {
      setDeriving(false);
    }
  };

  const close = () => {
    setOpen(false);
    setEditing(null);
    setDeriveError(null);
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
              {/* A cron source can only ever send its own payload, so that IS the flow's
                  input — offered first, as the default. An empty message is not something
                  the source does, so "No input" is dropped for cron flows; other flows keep
                  it as their default. */}
              {cronPayload ? (
                <li>
                  <button
                    type="button"
                    onClick={() => void startFromCron()}
                    disabled={deriving}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-black/[0.04] disabled:opacity-50 dark:hover:bg-white/[0.06]"
                  >
                    <Clock size={13} className="shrink-0 text-zinc-400" />
                    <span className="truncate">As the cron source would send it</span>
                    <span className="ml-auto text-xs text-zinc-400">
                      {deriving ? "evaluating…" : "from payload"}
                    </span>
                  </button>
                </li>
              ) : (
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
              )}

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

            {deriveError && (
              <p className="border-t border-black/10 px-3 py-2 text-xs text-red-500 dark:border-white/10">
                Couldn’t derive the cron input: {deriveError}
              </p>
            )}

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
