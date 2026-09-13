import type { BlockNode, EditorDocument, FlowDoc } from "../model/document";

/**
 * Whether a flow can be run without anybody noticing — the gate on the editor
 * running flows by itself to learn what their messages look like.
 *
 * A run the user asked for may do whatever the flow says. A run the *editor* decided to
 * make must be unobservable outside the process: calling a REST endpoint is somebody
 * else's database, and sending a Slack message twice is not a performance problem.
 *
 * So this is an allow-list, and the direction is the design: a block type nobody has
 * vouched for — including every block added after this file was written — makes its flow
 * ineligible. The cost of being wrong that way is that the editor learns nothing until
 * the user runs the flow themselves. Err closed.
 */

/**
 * Block types that transform the message and nothing else.
 *
 * Every entry has been checked against its runtime implementation for two things: it
 * reaches no network, and it leaves nothing behind that a later run could read. The
 * second is why `object-write`, `object-delete`, `cache-scope` and `invalidate-cache`
 * are absent though they never leave the machine — they mutate the runtime store.
 *
 * `log` is the one entry that binds a connector: the worst a background run can do
 * through it is write lines nobody asked for.
 */
export const PURE_BLOCKS: ReadonlySet<string> = new Set([
  // Data
  "set-payload",
  "set-variable",
  "delete-variable",
  "multi-transform",
  "template-resource",
  "object-read",
  // Flow control — the composites themselves add nothing; their slots are walked.
  "if",
  "switch",
  "foreach",
  "fork",
  "enrich",
  "split",
  "aggregate",
  "validate",
  "handle-errors",
  // Shaping what a webhook sent; neither calls back to the service that sent it.
  "notion-page-to-markdown",
  "notion-event",
  "slack-event",
  // Diagnostics only — see above.
  "log",
]);

/**
 * `flow-ref` is pure exactly when its target is, so it is resolved rather than
 * listed. The field holding the target flow's name, per the runtime's schema.
 */
const FLOW_REF_FIELD = "flow";

function blockIsPure(block: BlockNode, doc: EditorDocument, seen: Set<string>): boolean {
  if (block.type === "flow-ref") {
    const target = block.settings[FLOW_REF_FIELD];
    if (typeof target !== "string" || target === "") return false;
    const flow = doc.flows.find((f) => f.name === target);
    // A reference to a flow this document does not have is not a reference we can
    // clear, and a cycle is not one we can finish walking. Both fail closed.
    return flow ? flowIsPure(flow, doc, seen) : false;
  }
  if (!PURE_BLOCKS.has(block.type)) return false;
  // A composite is only as pure as what it holds. Slots are uniform in the model
  // (every one is a list of sub-flows), so this needs no per-type knowledge.
  for (const slot of Object.values(block.slots ?? {})) {
    for (const sub of slot) {
      if (!flowIsPure(sub, doc, seen)) return false;
    }
  }
  return true;
}

function flowIsPure(flow: FlowDoc, doc: EditorDocument, seen: Set<string>): boolean {
  // Re-entering a flow already on the stack means a cycle; refuse rather than recurse.
  // The diamond case (two flow-refs to one pure flow) is refused too, which is rare
  // enough not to be worth a second visited set.
  if (seen.has(flow.id)) return false;
  seen.add(flow.id);
  const chains = [flow.process, ...(flow.error ? [flow.error.process] : [])];
  const pure = chains.every((chain) => chain.every((b) => blockIsPure(b, doc, seen)));
  seen.delete(flow.id);
  return pure;
}

/**
 * Whether the editor may run this flow on its own.
 *
 * The flow's *source* is not considered: an editor-made run invokes the flow directly
 * and no source ever fires. What it does once it has the message is the only question.
 */
export function isPureFlow(doc: EditorDocument, flowId: string): boolean {
  const flow = doc.flows.find((f) => f.id === flowId);
  return flow ? flowIsPure(flow, doc, new Set()) : false;
}
