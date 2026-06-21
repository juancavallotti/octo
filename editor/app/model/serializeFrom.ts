import {
  BlockNode,
  ConnectorInstance,
  EditorDocument,
  FlowDoc,
  SourceNode,
  emptyDocument,
  isComposite,
  newId,
  withErrorChain,
} from "./document";
import { envFromRuntime } from "./serializeEnv";
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

/**
 * The runtime-config -> editor-document direction of the mapping (see serialize.ts
 * for the doc comment and the opposite direction). Kept in its own module so each
 * direction stays focused; this half never calls the to-runtime half.
 */

function connectorFromRuntime(c: RuntimeConnector): ConnectorInstance {
  return {
    id: newId(),
    name: c.name ?? "",
    type: c.type ?? "",
    settings: c.settings ?? {},
  };
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
    } else if (field.type === "route-list") {
      const list = (raw[field.name] as RuntimeRoute[] | undefined) ?? [];
      slots[field.name] = list.map((f) => routeFromRuntime(f, connTypes));
    } else if (field.type === "tool-list") {
      const list = (raw[field.name] as RuntimeTool[] | undefined) ?? [];
      slots[field.name] = list.map((f) => toolFromRuntime(f, connTypes));
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
  if (flow.error) {
    const process = flow.error.map((b) => blockFromRuntime(b, connTypes));
    out.error = { id: newId(), name: "", process };
  }
  return out;
}

function caseFromRuntime(
  flow: RuntimeCase,
  connTypes: Map<string, string>,
): FlowDoc {
  return { ...flowFromRuntime(flow, connTypes), when: flow.when ?? "" };
}

function routeFromRuntime(
  route: RuntimeRoute,
  connTypes: Map<string, string>,
): FlowDoc {
  return {
    ...flowFromRuntime(route, connTypes),
    description: route.description ?? "",
  };
}

function toolFromRuntime(
  tool: RuntimeTool,
  connTypes: Map<string, string>,
): FlowDoc {
  const out: FlowDoc = {
    ...flowFromRuntime(tool, connTypes),
    description: tool.description ?? "",
  };
  if (tool.inputSchema) out.inputSchema = tool.inputSchema;
  return out;
}

export function fromConfig(config: RuntimeConfig): EditorDocument {
  const env = (config.env ?? []).map(envFromRuntime);
  const connectors = (config.connectors ?? []).map(connectorFromRuntime);
  const connTypes = new Map(connectors.map((c) => [c.name, c.type]));
  // Top-level flows always carry an error chain so the canvas shows the lane;
  // seed an empty one for flows that declared no error path.
  const flows = (config.flows ?? []).map((f) =>
    withErrorChain(flowFromRuntime(f, connTypes)),
  );
  if (flows.length === 0) return { ...emptyDocument(), connectors, env };
  return { flows, connectors, env, processors: [] };
}
