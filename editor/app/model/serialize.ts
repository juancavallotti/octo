import {
  BlockNode,
  EditorDocument,
  FlowDoc,
  SourceNode,
  emptyDocument,
  isComposite,
  newId,
} from "./document";
import { getBlockSpec } from "@/app/schema";

/**
 * Maps the editor document to/from the runtime config shape (the YAML/JSON the
 * runtime loads — see runtime/types/flow.go). This keeps the model honest: it can
 * round-trip a file or start from scratch. Actual disk I/O is wired separately.
 *
 * The mapping is recursive: a composite block's slot fields (flow/flow-list/
 * case-list) become the runtime's typed slots (then/else/main/alternative/
 * branches/cases/default/body), each holding nested flows; its scalar fields
 * (condition/items/as) become top-level block keys; leaf blocks keep their
 * settings map.
 */

export interface RuntimeFlow {
  name?: string;
  source?: RuntimeSource;
  process?: RuntimeBlock[];
}

export interface RuntimeCase extends RuntimeFlow {
  when?: string;
}

export interface RuntimeSource {
  connector?: string;
  type?: string;
  settings?: Record<string, unknown>;
}

export interface RuntimeBlock {
  type?: string;
  name?: string;
  settings?: Record<string, unknown>;
  // Composite slots (nested flows).
  main?: RuntimeFlow;
  alternative?: RuntimeFlow;
  branches?: RuntimeFlow[];
  then?: RuntimeFlow;
  else?: RuntimeFlow;
  cases?: RuntimeCase[];
  default?: RuntimeFlow;
  body?: RuntimeFlow;
  // Composite scalars.
  condition?: unknown;
  items?: unknown;
  as?: unknown;
}

export interface RuntimeConfig {
  flows?: RuntimeFlow[];
}

function hasKeys(o: Record<string, unknown>): boolean {
  return Object.keys(o).length > 0;
}

function sourceToRuntime(source: SourceNode): RuntimeSource {
  const out: RuntimeSource = {};
  if (source.connector) out.connector = source.connector;
  if (source.type) out.type = source.type;
  if (hasKeys(source.settings)) out.settings = source.settings;
  return out;
}

function flowToRuntime(flow: FlowDoc): RuntimeFlow {
  const out: RuntimeFlow = {};
  if (flow.name) out.name = flow.name;
  if (flow.source) out.source = sourceToRuntime(flow.source);
  out.process = flow.process.map(blockToRuntime);
  return out;
}

function caseToRuntime(flow: FlowDoc): RuntimeCase {
  return { when: flow.when ?? "", ...flowToRuntime(flow) };
}

function blockToRuntime(block: BlockNode): RuntimeBlock {
  const spec = getBlockSpec(block.type);
  if (!spec || !isComposite(block.type)) {
    const out: RuntimeBlock = { type: block.type };
    if (block.name) out.name = block.name;
    if (hasKeys(block.settings)) out.settings = block.settings;
    return out;
  }

  const out: Record<string, unknown> = { type: block.type };
  if (block.name) out.name = block.name;
  for (const field of spec.fields) {
    const slot = block.slots?.[field.name] ?? [];
    if (field.type === "flow") {
      if (slot[0]) out[field.name] = flowToRuntime(slot[0]);
    } else if (field.type === "flow-list") {
      if (slot.length) out[field.name] = slot.map(flowToRuntime);
    } else if (field.type === "case-list") {
      if (slot.length) out[field.name] = slot.map(caseToRuntime);
    } else {
      const v = block.settings[field.name];
      if (v !== undefined) out[field.name] = v;
    }
  }
  return out as RuntimeBlock;
}

export function toConfig(doc: EditorDocument): RuntimeConfig {
  return { flows: doc.flows.map(flowToRuntime) };
}

function blockFromRuntime(block: RuntimeBlock): BlockNode {
  const type = block.type ?? "";
  const spec = getBlockSpec(type);
  const node: BlockNode = {
    id: newId(),
    type,
    name: block.name,
    settings: { ...(block.settings ?? {}) },
  };
  if (!spec || !isComposite(type)) return node;

  const raw = block as Record<string, unknown>;
  const slots: Record<string, FlowDoc[]> = {};
  for (const field of spec.fields) {
    if (field.type === "flow") {
      const f = raw[field.name] as RuntimeFlow | undefined;
      slots[field.name] = f ? [flowFromRuntime(f)] : [];
    } else if (field.type === "flow-list") {
      const list = (raw[field.name] as RuntimeFlow[] | undefined) ?? [];
      slots[field.name] = list.map(flowFromRuntime);
    } else if (field.type === "case-list") {
      const list = (raw[field.name] as RuntimeCase[] | undefined) ?? [];
      slots[field.name] = list.map(caseFromRuntime);
    } else {
      const v = raw[field.name];
      if (v !== undefined) node.settings[field.name] = v;
    }
  }
  node.slots = slots;
  return node;
}

function flowFromRuntime(flow: RuntimeFlow): FlowDoc {
  const out: FlowDoc = {
    id: newId(),
    name: flow.name ?? "",
    process: (flow.process ?? []).map(blockFromRuntime),
  };
  if (flow.source) {
    out.source = {
      connector: flow.source.connector,
      type: flow.source.type,
      settings: flow.source.settings ?? {},
    };
  }
  return out;
}

function caseFromRuntime(flow: RuntimeCase): FlowDoc {
  return { ...flowFromRuntime(flow), when: flow.when ?? "" };
}

export function fromConfig(config: RuntimeConfig): EditorDocument {
  const flows = (config.flows ?? []).map(flowFromRuntime);
  if (flows.length === 0) return emptyDocument();
  return { flows, connectors: [], processors: [] };
}
