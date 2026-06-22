/**
 * Types describing the runtime capability schema. This is the editor-side mirror
 * of what the CLI will eventually emit (see capabilities.json). The JSON is the
 * source of truth for *data*; these types give it shape and the loader in
 * index.ts resolves React-specific bits (icons).
 */

/** Kinds of configurable field a block or connector setting can expose. */
export type FieldType =
  | "string"
  | "number"
  | "boolean"
  | "cel"
  | "enum"
  | "string-list"
  | "string-map"
  | "flow"
  | "flow-list"
  | "case-list"
  // A list of named, described sub-flows: ai-router's `routes`. Each entry serializes
  // as {name, description, ...inline flow}.
  | "route-list"
  // A list of named, described, schema-bearing sub-flows: ai-agent's `tools`. Each
  // entry serializes as {name, description, inputSchema?, ...inline flow}.
  | "tool-list"
  // A bare block chain (not wrapped in a sub-flow), serialized directly as a
  // list of blocks under its field name — e.g. handle-errors' process/error.
  | "block-list";

/**
 * Describes that a (string) field holds a *reference* to another named entity in
 * the document, so the editor can offer a dropdown of valid targets instead of a
 * free-text input. A connector reference is narrowed either to one connector type
 * or to a connector *category* (e.g. any "llm" provider), or it points at a flow.
 */
export type ReferenceSpec =
  | { kind: "connector"; connectorType: string }
  | { kind: "connector"; connectorCategory: string }
  | { kind: "flow" };

/** A single configurable field (a block setting, source setting, etc.). */
export interface FieldSpec {
  name: string;
  label: string;
  type: FieldType;
  required: boolean;
  default?: string | number | boolean;
  enum?: string[];
  description?: string;
  /** When set, the field references a named connection/flow (rendered as a dropdown). */
  ref?: ReferenceSpec;
}

/** Whether a block is a plain processor or a control-flow composite. */
export type BlockCategory = "processor" | "control-flow";

/** A block type the runtime supports as a step in a flow's process chain. */
export interface BlockSpec {
  type: string;
  label: string;
  category: BlockCategory;
  /** Name of a lucide icon, resolved to a component by the loader. */
  icon: string;
  description: string;
  fields: FieldSpec[];
}

/** A source type a connector can expose to drive a flow. */
export interface SourceSpec {
  type: string;
  label: string;
  /** Name of a lucide icon, resolved to a component by the loader. */
  icon?: string;
  fields: FieldSpec[];
}

/** A connector type the runtime supports. */
export interface ConnectorSpec {
  type: string;
  label: string;
  /** Name of a lucide icon, resolved to a component by the loader. */
  icon?: string;
  /**
   * Optional grouping so a field can reference any connector in a family rather
   * than one exact type — e.g. the LLM providers all carry `category: "llm"` so
   * the AI blocks accept any of them.
   */
  category?: string;
  settings: FieldSpec[];
  sources: SourceSpec[];
}

/** The full capability catalogue. */
export interface Capabilities {
  blocks: BlockSpec[];
  connectors: ConnectorSpec[];
}
