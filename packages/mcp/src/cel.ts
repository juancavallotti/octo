/**
 * A hand-authored catalogue of the CEL variables and custom functions available
 * to Octo message expressions (log messages, payloads, if/switch conditions,
 * connector fields, …), served by the `getCelFunctions` tool.
 *
 * SOURCE OF TRUTH — these live only as Go declarations in the runtime's single CEL
 * seam; there is no machine-readable export to transcribe from, so this table is
 * maintained by hand. When a `RegisterMessageExtension` is added/changed in Go,
 * update this file to match:
 *   - variables: runtime/core/expr/message.go (MessageVars)
 *   - toJson/fromJson: runtime/core/expr/json.go
 *   - toFormData/fromFormData: runtime/core/expr/formdata.go
 *   - templateResource: runtime/core/expr/template.go
 */

/** A variable in scope for every message CEL expression. */
export interface CelVariable {
  name: string;
  /** The CEL type it evaluates to. */
  type: string;
  summary: string;
}

/** A custom CEL function the runtime registers for message expressions. */
export interface CelFunction {
  name: string;
  /** Human-readable signature, e.g. `fromJson(string) -> dyn`. */
  signature: string;
  summary: string;
  /** A short worked expression. */
  example: string;
}

/** The standard variables every message expression may reference (MessageVars). */
export const CEL_VARIABLES: CelVariable[] = [
  {
    name: "body",
    type: "dyn",
    summary:
      "The decoded message body — JSON-native shapes (map, list, string, double, bool, null). In raw-content mode it's the { contentType, rawData } envelope.",
  },
  {
    name: "vars",
    type: "map(string, dyn)",
    summary:
      "The message's variables: values set by the source (e.g. path params, captured headers, query) and by set-variable/multi-transform blocks.",
  },
  {
    name: "eventID",
    type: "string",
    summary: "The message's unique event id.",
  },
  {
    name: "correlationID",
    type: "string",
    summary:
      "The message's correlation id (empty unless the source or a block set one), for tracing a message across flows.",
  },
  {
    name: "env",
    type: "map(string, string)",
    summary:
      "The resolved environment variables the integration declared, e.g. env.API_BASE_URL.",
  },
  {
    name: "now",
    type: "timestamp",
    summary:
      "The evaluation time (for a source payload, the trigger/fire time). Use with CEL time helpers, e.g. now - duration('1h').",
  },
];

/** The custom functions registered on top of the CEL standard library. */
export const CEL_FUNCTIONS: CelFunction[] = [
  {
    name: "toJson",
    signature: "toJson(dyn) -> string",
    summary: "Marshal any value to a compact JSON string.",
    example: `toJson(body)`,
  },
  {
    name: "fromJson",
    signature: "fromJson(string) -> dyn",
    summary:
      "Parse a JSON string back into a decoded value — e.g. revert a captured raw JSON body.",
    example: `fromJson(body.rawData)`,
  },
  {
    name: "toFormData",
    signature: "toFormData(dyn) -> string",
    summary:
      "Encode an object as an application/x-www-form-urlencoded string (array fields emit repeated keys). Only urlencoded, not multipart.",
    example: `toFormData({"q": "hello", "page": 2})`,
  },
  {
    name: "fromFormData",
    signature: "fromFormData(string) -> dyn",
    summary:
      "Parse an application/x-www-form-urlencoded string into an object (repeated keys become a list) — e.g. decode a raw form POST body.",
    example: `fromFormData(body.rawData)`,
  },
  {
    name: "templateResource",
    signature: "templateResource(string) -> string",
    summary:
      "Render a template resource (by id) against the current message; the template sees the in-scope variables (body, vars, env, …) via {{ env.NAME }} / {{ body.* }}.",
    example: `templateResource("welcome-email")`,
  },
];

/** Look up one function by name; undefined when unknown. */
export function getCelFunction(name: string): CelFunction | undefined {
  return CEL_FUNCTIONS.find((f) => f.name === name);
}
