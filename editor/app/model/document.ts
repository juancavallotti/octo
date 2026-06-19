import { getBlockSpec } from "@/app/schema";
import type { FieldSpec } from "@/app/schema/types";

/**
 * The in-memory editing model. These are editor-side types: every node carries a
 * stable client `id` (distinct from the runtime config, which is keyed by name
 * and order). The reducer mutates this document; serialize.ts maps it to/from the
 * runtime YAML/JSON config shape.
 *
 * The model is recursive, mirroring the runtime (FlowConfig -> []BlockConfig ->
 * composite slots -> FlowConfig): a composite block holds nested sub-flows in
 * `slots`, keyed by the slot's field name.
 */

export interface BlockNode {
  /** Stable client id; never serialized to the runtime config. */
  id: string;
  /** Block type, e.g. "log" or "set-payload". Matches a schema BlockSpec. */
  type: string;
  /** Optional human-readable step name. */
  name?: string;
  /** Block settings keyed by field name. */
  settings: Record<string, unknown>;
  /**
   * Nested sub-flows for composite blocks, keyed by the slot field name
   * (then/else/main/alternative/body/default/branches/cases). Every slot is a
   * list for uniformity: single-flow slots hold 0–1 entries, flow-list/case-list
   * hold N.
   */
  slots?: Record<string, FlowDoc[]>;
}

export interface SourceNode {
  /** Name of a configured connector instance. */
  connector?: string;
  /** Connector-specific source type. */
  type?: string;
  settings: Record<string, unknown>;
}

export interface FlowDoc {
  id: string;
  name: string;
  source?: SourceNode;
  process: BlockNode[];
  /** CEL guard for a switch-case sub-flow (the case's `when`). */
  when?: string;
}

/** Field types whose value is one or more nested sub-flows. */
export const SLOT_FIELD_TYPES = new Set(["flow", "flow-list", "case-list"]);

/** Whether a field's value is a nested sub-flow (managed on the canvas, not in the panel). */
export function isSlotField(field: FieldSpec): boolean {
  return SLOT_FIELD_TYPES.has(field.type);
}

export interface ConnectorInstance {
  id: string;
  name: string;
  type: string;
  settings: Record<string, unknown>;
}

export interface EditorDocument {
  flows: FlowDoc[];
  connectors: ConnectorInstance[];
  /** Reusable processors referenced by name from a flow's process chain. */
  processors: BlockNode[];
}

/** Generate a stable client id. */
export function newId(): string {
  return crypto.randomUUID();
}

/** Seed a block's settings from the schema's scalar field defaults. */
export function defaultSettings(type: string): Record<string, unknown> {
  const spec = getBlockSpec(type);
  if (!spec) return {};
  const settings: Record<string, unknown> = {};
  for (const field of spec.fields) {
    if (field.default !== undefined) settings[field.name] = field.default;
  }
  return settings;
}

/** The slot fields (nested sub-flows) of a block type, in schema order. */
export function slotFields(type: string): FieldSpec[] {
  const spec = getBlockSpec(type);
  if (!spec) return [];
  return spec.fields.filter((f) => SLOT_FIELD_TYPES.has(f.type));
}

/** Whether a block type nests sub-flows (if/switch/foreach/fork/scope). */
export function isComposite(type: string): boolean {
  return slotFields(type).length > 0;
}

/** Create a fresh block of the given type, seeding settings and composite slots. */
export function newBlock(type: string): BlockNode {
  const block: BlockNode = { id: newId(), type, settings: defaultSettings(type) };
  const fields = slotFields(type);
  if (fields.length > 0) {
    const slots: Record<string, FlowDoc[]> = {};
    // Seed every slot with one empty sub-flow so there is somewhere to drop.
    for (const field of fields) slots[field.name] = [emptyFlow("")];
    block.slots = slots;
  }
  return block;
}

/** An empty flow with no source and no steps. */
export function emptyFlow(name = "New flow"): FlowDoc {
  return { id: newId(), name, process: [] };
}

/** A document with a single empty flow — the "new file" template / test baseline. */
export function emptyDocument(): EditorDocument {
  return { flows: [emptyFlow()], connectors: [], processors: [] };
}

/** A truly empty document — no flows at all (the editor's scratch start state). */
export function blankDocument(): EditorDocument {
  return { flows: [], connectors: [], processors: [] };
}

/** Recursively transform a sub-flow inside a block's slots, returning a copy. */
function mapBlock(block: BlockNode, flowId: string, fn: FlowFn): BlockNode {
  if (!block.slots) return block;
  const slots: Record<string, FlowDoc[]> = {};
  for (const [name, subs] of Object.entries(block.slots)) {
    slots[name] = subs.map((f) => mapFlowTree(f, flowId, fn));
  }
  return { ...block, slots };
}

type FlowFn = (flow: FlowDoc) => FlowDoc;

/** Apply `fn` to the flow with `flowId` anywhere in the tree, returning a copy. */
function mapFlowTree(flow: FlowDoc, flowId: string, fn: FlowFn): FlowDoc {
  if (flow.id === flowId) return fn(flow);
  return { ...flow, process: flow.process.map((b) => mapBlock(b, flowId, fn)) };
}

/**
 * Return a new document with `fn` applied to the flow identified by `flowId`,
 * wherever it lives — a top-level flow or one nested in a composite's slot.
 */
export function mapFlow(
  doc: EditorDocument,
  flowId: string,
  fn: FlowFn,
): EditorDocument {
  return { ...doc, flows: doc.flows.map((f) => mapFlowTree(f, flowId, fn)) };
}

type BlockFn = (block: BlockNode) => BlockNode;

/** Apply `fn` to one block (by id) inside a flow tree, returning a copy. */
function mapBlockTree(flow: FlowDoc, blockId: string, fn: BlockFn): FlowDoc {
  const process = flow.process.map((block) => {
    const next = block.id === blockId ? fn(block) : block;
    if (!next.slots) return next;
    const slots: Record<string, FlowDoc[]> = {};
    for (const [name, subs] of Object.entries(next.slots)) {
      slots[name] = subs.map((f) => mapBlockTree(f, blockId, fn));
    }
    return { ...next, slots };
  });
  return { ...flow, process };
}

/**
 * Return a new document with `fn` applied to the block identified by `blockId`,
 * wherever it lives — in a top-level flow or nested in a composite's slot.
 */
export function mapBlockById(
  doc: EditorDocument,
  blockId: string,
  fn: BlockFn,
): EditorDocument {
  return { ...doc, flows: doc.flows.map((f) => mapBlockTree(f, blockId, fn)) };
}

/** Find a block by id anywhere in the tree (top-level or nested in a slot). */
export function findBlock(
  doc: EditorDocument,
  blockId: string,
): BlockNode | undefined {
  const visit = (flow: FlowDoc): BlockNode | undefined => {
    for (const block of flow.process) {
      if (block.id === blockId) return block;
      if (!block.slots) continue;
      for (const subs of Object.values(block.slots)) {
        for (const sub of subs) {
          const hit = visit(sub);
          if (hit) return hit;
        }
      }
    }
    return undefined;
  };
  for (const flow of doc.flows) {
    const hit = visit(flow);
    if (hit) return hit;
  }
  return undefined;
}

/** Find a flow by id anywhere in the tree (top-level or nested in a slot). */
export function findFlow(
  doc: EditorDocument,
  flowId: string,
): FlowDoc | undefined {
  const visit = (flow: FlowDoc): FlowDoc | undefined => {
    if (flow.id === flowId) return flow;
    for (const block of flow.process) {
      if (!block.slots) continue;
      for (const subs of Object.values(block.slots)) {
        for (const sub of subs) {
          const hit = visit(sub);
          if (hit) return hit;
        }
      }
    }
    return undefined;
  };
  for (const flow of doc.flows) {
    const hit = visit(flow);
    if (hit) return hit;
  }
  return undefined;
}
