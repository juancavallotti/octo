"use client";

import { useEffect, useRef, useState } from "react";
import { toRunnableYaml } from "../model/runConfig";
import { sourcePayloadExpression } from "../model/sourcePayload";
import { useEditorPrefs } from "../prefs/prefs";
import { useEditorMeta } from "../providers/EditorMetaProvider";
import { currentFlowId } from "../state/currentFlow";
import { useEditorState } from "../state/editorState";
import { isPureFlow } from "./pure";
import type { RunTransport } from "./transport";

/**
 * Runs the open flow in the background, when doing so costs nothing, so CEL completion
 * knows what its messages look like before the user has run anything.
 *
 * The knowledge this produces cannot be had by reading the document: what a block leaves
 * behind depends on how it is configured, and only running it answers that reliably.
 *
 * Three things keep it unobtrusive:
 *
 *  - **It is opt-in** (`prefs.autoLearn`), because "the editor runs your flows" is a
 *    sentence the user should have agreed to.
 *  - **It is invisible.** It calls the transport directly rather than going through
 *    FlowRunProvider's `run`, so it records no history entry, opens no console tab and
 *    spins no block.
 *  - **It is silent about failure.** A background run that cannot complete teaches
 *    nothing, and reporting it would interrupt the user about a run they did not start.
 */

/** How long the document must sit still before a background run is worth making. */
const SETTLE_MS = 1200;

/** Joins the parts of a run signature; not a character any of them can contain. */
const SEP = "\u0000";

export default function AutoLearn({ transport }: { transport: RunTransport }) {
  const { autoLearn } = useEditorPrefs();
  const { state } = useEditorState();
  const meta = useEditorMeta();
  const doc = state.document;
  // The same "which flow does the user mean" rule Cmd+Enter follows, so the editor
  // never learns about one flow while the user is working in another.
  const flowId = currentFlowId(state);
  const integrationId = state.integration.id;

  /**
   * What was last run, so an edit elsewhere in the document — or simply moving between
   * tabs — does not re-run a flow whose answer we already have. Keyed by what was
   * actually sent, so a flow edited back to a shape already run is correctly skipped.
   */
  const ranRef = useRef<string | null>(null);
  /** One background run at a time, whatever the user does while it is in flight. */
  const busyRef = useRef(false);
  /**
   * Bumped when a run settles, so the effect reconsiders the document as it stands now.
   * Without it, an edit made WHILE a run is in flight is dropped: its timer fires, sees
   * `busyRef`, returns, and nothing reschedules it.
   */
  const [settled, setSettled] = useState(0);

  useEffect(() => {
    if (!autoLearn || !meta || !flowId) return;
    const flow = doc.flows.find((f) => f.id === flowId);
    // An empty flow has nothing to teach: the only message it would see is the one sent.
    if (!flow || flow.process.length === 0) return;
    if (!isPureFlow(doc, flowId)) return;

    // The first saved input when there is one — a flow's real input is a better probe
    // than an empty message, and it is what the user has already said the flow expects.
    // Failing that, the body the source itself would synthesize: a cron flow's whole
    // input is its payload expression, and running one with an empty message teaches
    // nothing, because every expression downstream reads a body that was never there.
    const input = meta.inputs(flowId)[0];
    const payload = input ? null : sourcePayloadExpression(flow.source);
    const yaml = toRunnableYaml(doc);
    // The input's CONTENT, not just its id: editing a saved input in place keeps its
    // id, so keying on the id alone means the flow is never re-run and completion goes
    // on offering shapes learned from the body the user just replaced.
    const signature = [flow.name, input?.id ?? "", input?.data ?? "", input?.vars ?? "", yaml].join(SEP);
    if (ranRef.current === signature) return;

    const timer = setTimeout(() => {
      if (busyRef.current) return;
      busyRef.current = true;
      // Marked as run before the call rather than after: a flow that fails must not be
      // retried on every keystroke, and a failure is as final as a success here.
      ranRef.current = signature;
      // The payload is CEL (it may read `now`), so it is evaluated through the runner
      // rather than by this bundle pretending to know what CEL means.
      void derive(payload, transport)
        .then((data) =>
          transport.invoke({
            yaml,
            flow: flow.name,
            integrationId: integrationId ?? undefined,
            data: input?.data ?? data,
            vars: input?.vars,
            learnShapes: true,
          }),
        )
        .then((outcome) => {
          // The shapes are the entire point; the output, the logs and whether it even
          // succeeded are all discarded. Blocks that ran before a failure carried real
          // messages, so a failed run is still worth reading.
          if (outcome.shapes) meta.learn(outcome.shapes);
        })
        .catch(() => {
          // Silent — see the header.
        })
        .finally(() => {
          busyRef.current = false;
          // Reconsider the document as it stands now — it may have moved on while this
          // run was in flight.
          setSettled((n) => n + 1);
        });
    }, SETTLE_MS);
    return () => clearTimeout(timer);
  }, [autoLearn, meta, doc, flowId, transport, integrationId, settled]);

  return null;
}

/**
 * The body a source's payload expression evaluates to, as JSON, or undefined.
 *
 * An expression that will not evaluate resolves to undefined rather than rejecting: the
 * run is still worth making with an empty body, and there is nobody to tell.
 */
async function derive(
  expression: string | null,
  transport: RunTransport,
): Promise<string | undefined> {
  if (!expression) return undefined;
  try {
    const res = await transport.evalCel({ expression });
    if (!res.ok || res.error !== undefined) return undefined;
    return JSON.stringify(res.result ?? null);
  } catch {
    return undefined;
  }
}
