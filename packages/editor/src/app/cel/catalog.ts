/**
 * A hand-authored catalogue of what a CEL expression can reference in the editor:
 * the in-scope message variables, the custom functions Octo registers, and the
 * common standard-CEL macros/functions. It drives both autocomplete and the
 * IDE-style hover docs in the CEL fields and the CEL tester.
 *
 * SOURCE OF TRUTH — the Octo-specific entries (variables + OCTO_FUNCTIONS) mirror
 * the Go declarations in the runtime's single CEL seam; there is no machine-readable
 * export to transcribe from yet, so they are kept in sync by hand. This table is a
 * near-copy of the MCP catalogue at `packages/mcp/src/cel.ts` — when a
 * `RegisterMessageExtension` is added/changed in Go, update BOTH:
 *   - variables:                 runtime/core/expr/message.go (MessageVars)
 *   - toJson / fromJson:         runtime/core/expr/json.go
 *   - toFormData / fromFormData: runtime/core/expr/formdata.go
 *   - templateResource:          runtime/core/expr/template.go
 * The CEL_BUILTINS entries are standard CEL and change only with the CEL spec
 * (reference: https://celbyexample.com/).
 *
 * Follow-up (issue #125 → #120): source this catalogue from the runtime and make it
 * context-aware (block-scoped variables, member/type completion) instead of this
 * hand-authored, static table.
 */

/** Whether a completion is a value in scope or something callable. */
export type CelKind = "variable" | "function";

/** One entry the completion menu and hover docs render. */
export interface CelEntry {
  name: string;
  kind: CelKind;
  /** For variables, the CEL type (e.g. `map(string, dyn)`); for functions, a signature. */
  signature: string;
  summary: string;
  /** A short worked expression using the entry. */
  example: string;
}

/** The standard variables every message expression may reference (Go: MessageVars). */
export const CEL_VARIABLES: CelEntry[] = [
  {
    name: "body",
    kind: "variable",
    signature: "dyn",
    summary:
      "The decoded message body — JSON-native shapes (map, list, string, double, bool, null). In raw-content mode it's the { contentType, rawData } envelope.",
    example: "body.user.id",
  },
  {
    name: "vars",
    kind: "variable",
    signature: "map(string, dyn)",
    summary:
      "The message's variables: values set by the source (e.g. path params, captured headers, query) and by set-variable/multi-transform blocks.",
    example: "vars.userId",
  },
  {
    name: "eventID",
    kind: "variable",
    signature: "string",
    summary: "The message's unique event id.",
    example: "eventID",
  },
  {
    name: "correlationID",
    kind: "variable",
    signature: "string",
    summary:
      "The message's correlation id (empty unless the source or a block set one), for tracing a message across flows.",
    example: "correlationID != ''",
  },
  {
    name: "env",
    kind: "variable",
    signature: "map(string, string)",
    summary:
      "The resolved environment variables the integration declared, e.g. env.API_BASE_URL.",
    example: "env.API_BASE_URL",
  },
  {
    name: "now",
    kind: "variable",
    signature: "timestamp",
    summary:
      "The evaluation time (for a source payload, the trigger/fire time). Use with CEL time helpers, e.g. now - duration('1h').",
    example: "now - duration('1h')",
  },
];

/** The custom functions Octo registers on top of the CEL standard library. */
export const OCTO_FUNCTIONS: CelEntry[] = [
  {
    name: "toJson",
    kind: "function",
    signature: "toJson(dyn) -> string",
    summary: "Marshal any value to a compact JSON string.",
    example: "toJson(body)",
  },
  {
    name: "fromJson",
    kind: "function",
    signature: "fromJson(string) -> dyn",
    summary:
      "Parse a JSON string back into a decoded value — e.g. revert a captured raw JSON body.",
    example: "fromJson(body.rawData)",
  },
  {
    name: "toFormData",
    kind: "function",
    signature: "toFormData(dyn) -> string",
    summary:
      "Encode an object as an application/x-www-form-urlencoded string (array fields emit repeated keys). Only urlencoded, not multipart.",
    example: 'toFormData({"q": "hello", "page": 2})',
  },
  {
    name: "fromFormData",
    kind: "function",
    signature: "fromFormData(string) -> dyn",
    summary:
      "Parse an application/x-www-form-urlencoded string into an object (repeated keys become a list) — e.g. decode a raw form POST body.",
    example: "fromFormData(body.rawData)",
  },
  {
    name: "templateResource",
    kind: "function",
    signature: "templateResource(string) -> string",
    summary:
      "Render a template resource (by id) against the current message; the template sees the in-scope variables via {{ env.NAME }} / {{ body.* }}. (Not available in the standalone CEL tester.)",
    example: 'templateResource("welcome-email")',
  },
];

/**
 * Common standard-CEL macros and functions. Curated — not the entire stdlib — to
 * the operations authors reach for most. Macros (has/all/exists/map/filter) and
 * receiver-style methods (startsWith/contains/…) are listed by their bare name.
 */
export const CEL_BUILTINS: CelEntry[] = [
  {
    name: "has",
    kind: "function",
    signature: "has(field) -> bool",
    summary:
      "Presence test macro: true when the field/key exists and is set. Guards access to optional fields.",
    example: "has(body.email)",
  },
  {
    name: "size",
    kind: "function",
    signature: "size(string|bytes|list|map) -> int",
    summary: "The length of a string, byte sequence, list, or map.",
    example: "size(body.items) > 0",
  },
  {
    name: "all",
    kind: "function",
    signature: "list.all(x, predicate) -> bool",
    summary: "Macro: true when the predicate holds for every element.",
    example: "body.items.all(x, x.qty > 0)",
  },
  {
    name: "exists",
    kind: "function",
    signature: "list.exists(x, predicate) -> bool",
    summary: "Macro: true when the predicate holds for at least one element.",
    example: "body.roles.exists(r, r == 'admin')",
  },
  {
    name: "exists_one",
    kind: "function",
    signature: "list.exists_one(x, predicate) -> bool",
    summary: "Macro: true when the predicate holds for exactly one element.",
    example: "body.items.exists_one(x, x.primary)",
  },
  {
    name: "map",
    kind: "function",
    signature: "list.map(x, expr) -> list",
    summary: "Macro: build a new list by evaluating expr for each element.",
    example: "body.items.map(x, x.name)",
  },
  {
    name: "filter",
    kind: "function",
    signature: "list.filter(x, predicate) -> list",
    summary: "Macro: keep only the elements for which the predicate is true.",
    example: "body.items.filter(x, x.qty > 0)",
  },
  {
    name: "contains",
    kind: "function",
    signature: "string.contains(substr) -> bool",
    summary: "True when the string contains the given substring.",
    example: "body.subject.contains('urgent')",
  },
  {
    name: "startsWith",
    kind: "function",
    signature: "string.startsWith(prefix) -> bool",
    summary: "True when the string begins with the given prefix.",
    example: "body.path.startsWith('/api/')",
  },
  {
    name: "endsWith",
    kind: "function",
    signature: "string.endsWith(suffix) -> bool",
    summary: "True when the string ends with the given suffix.",
    example: "body.file.endsWith('.pdf')",
  },
  {
    name: "matches",
    kind: "function",
    signature: "string.matches(regex) -> bool",
    summary: "True when the string matches the RE2 regular expression.",
    example: "body.email.matches('^[^@]+@[^@]+$')",
  },
  {
    name: "timestamp",
    kind: "function",
    signature: "timestamp(string) -> timestamp",
    summary: "Parse an RFC 3339 string into a timestamp.",
    example: "timestamp('2026-01-01T00:00:00Z')",
  },
  {
    name: "duration",
    kind: "function",
    signature: "duration(string) -> duration",
    summary: "Parse a duration string (e.g. '1h', '30m', '1h30m') into a duration.",
    example: "now - duration('24h')",
  },
  {
    name: "int",
    kind: "function",
    signature: "int(dyn) -> int",
    summary: "Convert a number, string, timestamp, or enum to an int.",
    example: "int(body.count)",
  },
  {
    name: "uint",
    kind: "function",
    signature: "uint(dyn) -> uint",
    summary: "Convert a value to an unsigned int.",
    example: "uint(body.count)",
  },
  {
    name: "double",
    kind: "function",
    signature: "double(dyn) -> double",
    summary: "Convert an int, uint, or string to a double.",
    example: "double(body.price)",
  },
  {
    name: "string",
    kind: "function",
    signature: "string(dyn) -> string",
    summary: "Convert a value (number, bool, bytes, timestamp, duration) to a string.",
    example: "string(body.count)",
  },
  {
    name: "bool",
    kind: "function",
    signature: "bool(string) -> bool",
    summary: "Parse 'true'/'false' (and 1/0 variants) into a bool.",
    example: "bool(vars.enabled)",
  },
  {
    name: "bytes",
    kind: "function",
    signature: "bytes(string) -> bytes",
    summary: "Convert a UTF-8 string to a byte sequence.",
    example: "size(bytes(body.text))",
  },
  {
    name: "type",
    kind: "function",
    signature: "type(dyn) -> type",
    summary: "The runtime type of a value, e.g. type(body) == map.",
    example: "type(body.id) == string",
  },
  {
    name: "dyn",
    kind: "function",
    signature: "dyn(x) -> dyn",
    summary: "Erase static type information, forcing dynamic dispatch on x.",
    example: "dyn(body.value)",
  },
];

/** Every completion, variables first then functions. */
export function allCompletions(): CelEntry[] {
  return [...CEL_VARIABLES, ...OCTO_FUNCTIONS, ...CEL_BUILTINS];
}

/** Look up one entry by exact name; undefined when unknown. */
export function lookup(name: string): CelEntry | undefined {
  return allCompletions().find((e) => e.name === name);
}

/** Resolve a set of catalogue entries by name (drops any unknown name). */
function pick(names: string[]): CelEntry[] {
  return names.map((n) => lookup(n)).filter((e): e is CelEntry => e !== undefined);
}

/** Receiver-style macros/functions for a list value (e.g. `items.map(...)`). */
export const LIST_METHODS: CelEntry[] = pick([
  "all",
  "exists",
  "exists_one",
  "map",
  "filter",
  "size",
]);

/** Receiver-style methods for a string value (e.g. `path.startsWith(...)`). */
export const STRING_METHODS: CelEntry[] = pick([
  "startsWith",
  "endsWith",
  "contains",
  "matches",
  "size",
]);
