"use client";

import { useState } from "react";
import { Play } from "lucide-react";
import { Popover } from "../../components/ui";
import { useRun } from "../run/RunContext";
import { useFlowRun } from "../run/FlowRunContext";
import { useEditorMeta } from "../providers/EditorMetaProvider";
import { issueMessages } from "../model/validate";
import type { TestInput } from "../meta/types";
import FlowRunMenuItems from "./FlowRunMenuItems";
import TestInputForm from "./TestInputForm";

/**
 * The play button on a flow card, and the menu it opens: every way to run this flow —
 * with no input, with a saved one, or as one of the scenarios its test suite declares.
 *
 * A flow can always be run with no input (an empty message) — the commonest case for a
 * flow whose source supplies nothing anyway — so that is the first entry rather than
 * something the user has to construct.
 *
 * Renders nothing without a run capability. When the document is invalid, the button is
 * disabled and says why: the runner would only reject it.
 *
 * The menu's contents live in FlowRunMenuItems; this file owns the button, the popover
 * and the one thing that replaces both — the input form.
 */
export default function FlowRunMenu({ flowId }: { flowId: string }) {
  const run = useRun();
  const flowRun = useFlowRun();
  const meta = useEditorMeta();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<TestInput | "new" | null>(null);

  if (!run?.available || !flowRun) return null;

  const blocked = !run.validation.ok;
  const title = blocked
    ? `Fix before running:\n• ${issueMessages(run.validation).join("\n• ")}`
    : "Run this flow";

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
          <FlowRunMenuItems flowId={flowId} onClose={() => setOpen(false)} onEdit={setEditing} />
        )}
      </div>
    </Popover>
  );
}
