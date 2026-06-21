import {
  BlockNode,
  ConnectorInstance,
  EditorDocument,
  FlowDoc,
  SourceNode,
  isComposite,
} from "./document";
import { connectorResolver, type ConnectorResolver } from "./connectors";
import { envToRuntime } from "./serializeEnv";
import {
  type RuntimeBlock,
  type RuntimeCase,
  type RuntimeConfig,
  type RuntimeConnector,
  type RuntimeFlow,
  type RuntimeRoute,
  type RuntimeSource,
  type RuntimeTool,
} from "./serializeTypes";
import { getBlockSpec } from "@/app/schema";
import { fromConfig } from "./serializeFrom";

/**
 * Maps the editor document to/from the runtime config shape (the YAML/JSON the
 * runtime loads — see runtime/types/flow.go), so the model can round-trip a file
 * or start from scratch. The mapping is recursive: a composite block's slot
 * fields (flow/flow-list/case-list/route-list/tool-list) become the runtime's
 * typed slots, block-list fields (handle-errors' process/error) become bare block
 * lists, and its scalar fields become top-level keys; leaf blocks keep their
 * settings map. This file holds the document -> runtime direction; the inverse
 * lives in serializeFrom.ts and is re-exported here for a single entry point.
 */

export { fromConfig };

export type {
  RuntimeFlow,
  RuntimeCase,
  RuntimeRoute,
  RuntimeTool,
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
  // The flow-level error path serializes as a bare block list, only when set.
  if (flow.error && flow.error.process.length) {
    out.error = flow.error.process.map((b) => blockToRuntime(b, resolve));
  }
  return out;
}

function caseToRuntime(flow: FlowDoc, resolve: ConnectorResolver): RuntimeCase {
  return { when: flow.when ?? "", ...flowToRuntime(flow, resolve) };
}

// An ai-router route: the inline flow plus the description the model reads. The
// route name rides on the inline flow's `name` (the runtime's shared key).
function routeToRuntime(flow: FlowDoc, resolve: ConnectorResolver): RuntimeRoute {
  return { ...flowToRuntime(flow, resolve), description: flow.description ?? "" };
}

// An ai-agent tool: like a route, plus the JSON Schema for its arguments.
function toolToRuntime(flow: FlowDoc, resolve: ConnectorResolver): RuntimeTool {
  const out: RuntimeTool = {
    ...flowToRuntime(flow, resolve),
    description: flow.description ?? "",
  };
  if (flow.inputSchema) out.inputSchema = flow.inputSchema;
  return out;
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
    } else if (field.type === "route-list") {
      if (slot.length) out[field.name] = slot.map((f) => routeToRuntime(f, resolve));
    } else if (field.type === "tool-list") {
      if (slot.length) out[field.name] = slot.map((f) => toolToRuntime(f, resolve));
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
