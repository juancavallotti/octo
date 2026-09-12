import { rootFlowIdOf } from "../model/document";
import type { EditorState } from "./reducer";

/**
 * The flow the user means, when something acts on "the" flow without being told which.
 *
 * The canvas shows every flow at once, so there is no such thing as the open one and
 * the answer has to be assembled from what the user last touched: a selected block
 * names its flow (its *root* flow — a block inside a composite still belongs to the
 * top-level flow that runs it), then a selected source, then the flow last clicked
 * into, and finally the only flow there is, because a document with one flow can mean
 * nothing else.
 *
 * Shared rather than duplicated: Cmd+Enter runs this flow and the background
 * shape-learning runs this flow, and two answers to "which flow" would show up as the
 * editor learning about one flow while the user is running another.
 */
export function currentFlowId(state: EditorState): string | null {
  const doc = state.document;
  const selected = state.selectedBlockId ? rootFlowIdOf(doc, state.selectedBlockId) : null;
  if (selected) return selected;
  if (state.selectedSourceFlowId) return state.selectedSourceFlowId;
  if (state.activeFlowId) return state.activeFlowId;
  return doc.flows.length === 1 ? doc.flows[0].id : null;
}
