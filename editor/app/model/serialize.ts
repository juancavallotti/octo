import {
  BlockNode,
  ConnectorInstance,
  EditorDocument,
  FlowDoc,
  SourceNode,
  emptyDocument,
  isComposite,
  newId,
} from "./document";
import { connectorResolver, type ConnectorResolver } from "./connectors";
import { envFromRuntime, envToRuntime } from "./serializeEnv";
import {
  type RuntimeBlock,
  type RuntimeCase,
  type RuntimeConfig,
  type RuntimeConnector,
  type RuntimeFlow,
  type RuntimeSource,
} from "./serializeTypes";
import { getBlockSpec } from "@/app/schema";

/**
 * Maps the editor document to/from the runtime config shape (the YAML/JSON the
 * runtime loads — see runtime/types/flow.go), so the model can round-trip a file
 * or start from scratch. The mapping is recursive: a composite block's slot
 * fields (flow/flow-list/case-list) become the runtime's typed slots, block-list
 * fields (handle-errors' process/error) become bare block lists, and its scalar
 * fields become top-level keys; leaf blocks keep their settings map.
 */

export type {
  RuntimeFlow,
  RuntimeCase,
  RuntimeSource,
  RuntimeBlock,
  RuntimeConnector,
  RuntimeConfig,
};

const hasKeys = (o: Record<string, unknown>): boolean =>
  Object.keys(o).length > 0;

function connectorToRuntime(c: ConnectorInstance): RuntimeConnector {
  const out: RuntimeConnector = {};
  if (c.name) out.name = c.name;
  if (c.type) out.type = c.type;
  if (hasKeys(c.settings)) out.settings = c.settings;
  return out;
}

function connectorFromRuntime(c: RuntimeConnector): ConnectorInstance {
  return {
    id: newId(),
    name: c.name ?? "",
    type: c.type ?? "",
    settings: c.settings ?? {},
  };
}

function sourceToRuntime(
  source: SourceNode,
  resolve: ConnectorResolver,
): RuntimeSource {
  const out: RuntimeSource = {};
  const connector = resolve(source);
  if (connector) out.connector = connector;
  if (source.type) out.type = source.type;
  if (hasKeys(source.settings)) out.settings = source.settings;
  return out;
}

function flowToRuntime(flow: FlowDoc, resolve: ConnectorResolver): RuntimeFlow {
  const out: RuntimeFlow = {};
  if (flow.name) out.name = flow.name;
  if (flow.source) out.source = sourceToRuntime(flow.source, resolve);
  out.process = flow.process.map((b) => blockToRuntime(b, resolve));
  return out;
}

function caseToRuntime(flow: FlowDoc, resolve: ConnectorResolver): RuntimeCase {
  return { when: flow.when ?? "", ...flowToRuntime(flow, resolve) };
}

function blockToRuntime(
  block: BlockNode,
  resolve: ConnectorResolver,
): RuntimeBlock {
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
      if (slot[0]) out[field.name] = flowToRuntime(slot[0], resolve);
    } else if (field.type === "flow-list") {
      if (slot.length) out[field.name] = slot.map((f) => flowToRuntime(f, resolve));
    } else if (field.type === "case-list") {
      if (slot.length) out[field.name] = slot.map((f) => caseToRuntime(f, resolve));
    } else if (field.type === "block-list") {
      // A bare block chain: emit the held flow's process directly under the key.
      const blocks = slot[0]?.process ?? [];
      if (blocks.length) out[field.name] = blocks.map((b) => blockToRuntime(b, resolve));
    } else {
      const v = block.settings[field.name];
      if (v !== undefined) out[field.name] = v;
    }
  }
  return out as RuntimeBlock;
}

export function toConfig(doc: EditorDocument): RuntimeConfig {
  const out: RuntimeConfig = {};
  if (doc.env.length) out.env = doc.env.map(envToRuntime);
  if (doc.connectors.length) {
    out.connectors = doc.connectors.map(connectorToRuntime);
  }
  const resolve = connectorResolver(doc.connectors);
  out.flows = doc.flows.map((f) => flowToRuntime(f, resolve));
  return out;
}

function blockFromRuntime(
  block: RuntimeBlock,
  connTypes: Map<string, string>,
): BlockNode {
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
      slots[field.name] = f ? [flowFromRuntime(f, connTypes)] : [];
    } else if (field.type === "flow-list") {
      const list = (raw[field.name] as RuntimeFlow[] | undefined) ?? [];
      slots[field.name] = list.map((f) => flowFromRuntime(f, connTypes));
    } else if (field.type === "case-list") {
      const list = (raw[field.name] as RuntimeCase[] | undefined) ?? [];
      slots[field.name] = list.map((f) => caseFromRuntime(f, connTypes));
    } else if (field.type === "block-list") {
      // Wrap the bare block chain back into a single holding flow.
      const list = (raw[field.name] as RuntimeBlock[] | undefined) ?? [];
      const flow: FlowDoc = {
        id: newId(),
        name: "",
        process: list.map((b) => blockFromRuntime(b, connTypes)),
      };
      slots[field.name] = [flow];
    } else {
      const v = raw[field.name];
      if (v !== undefined) node.settings[field.name] = v;
    }
  }
  node.slots = slots;
  return node;
}

function sourceFromRuntime(
  source: RuntimeSource,
  connTypes: Map<string, string>,
): SourceNode {
  const name = source.connector ?? "";
  const out: SourceNode = {
    // Resolve the connector type from the bound instance; fall back to the raw
    // value for legacy configs that stored the type directly under `connector`.
    connector: connTypes.get(name) ?? (name || undefined),
    type: source.type,
    settings: source.settings ?? {},
  };
  // Only treat `name` as an explicit binding when it names a real connection.
  // When it's a type fallback (implicit default binding, no instance of that
  // name), leave connectorRef unset — otherwise it reads as a dangling reference
  // and the document round-trips into an invalid state.
  if (connTypes.has(name)) out.connectorRef = name;
  return out;
}

function flowFromRuntime(
  flow: RuntimeFlow,
  connTypes: Map<string, string>,
): FlowDoc {
  const out: FlowDoc = {
    id: newId(),
    name: flow.name ?? "",
    process: (flow.process ?? []).map((b) => blockFromRuntime(b, connTypes)),
  };
  if (flow.source) out.source = sourceFromRuntime(flow.source, connTypes);
  return out;
}

function caseFromRuntime(
  flow: RuntimeCase,
  connTypes: Map<string, string>,
): FlowDoc {
  return { ...flowFromRuntime(flow, connTypes), when: flow.when ?? "" };
}

export function fromConfig(config: RuntimeConfig): EditorDocument {
  const env = (config.env ?? []).map(envFromRuntime);
  const connectors = (config.connectors ?? []).map(connectorFromRuntime);
  const connTypes = new Map(connectors.map((c) => [c.name, c.type]));
  const flows = (config.flows ?? []).map((f) => flowFromRuntime(f, connTypes));
  if (flows.length === 0) return { ...emptyDocument(), connectors, env };
  return { flows, connectors, env, processors: [] };
}
