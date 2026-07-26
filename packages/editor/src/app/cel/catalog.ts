/**
 * A hand-authored catalogue of what a CEL expression can reference in the editor:
 * the in-scope message variables, the custom functions Octo registers, the cel-go
 * extension libraries it enables, and the common standard-CEL macros/functions. It
 * drives both autocomplete and the IDE-style hover docs in the CEL fields and the
 * CEL tester.
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
 *   - EXT_METHODS / EXT_NAMESPACES: runtime/core/expr/stdext.go, which pins the
 *     version of each cel-go library and therefore which functions exist.
 * The CEL_BUILTINS entries are standard CEL and change only with the CEL spec
 * (reference: https://celbyexample.com/).
 *
 * Follow-up (issue #125 → #120): source this catalogue from the runtime and make it
 * context-aware (block-scoped variables, member/type completion) instead of this
 * hand-authored, static table.
 */

/**
 * Whether a completion is a value in scope, something callable, or the prefix of a
 * namespaced function family (`math.`, `regex.`, …) whose members complete after
 * the dot.
 */
export type CelKind = "variable" | "function" | "namespace";

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
 * Receiver-style functions from the cel-go extension libraries the runtime enables
 * (strings, lists, two-variable comprehensions, optional). Called on a value:
 * `body.name.upperAscii()`, `body.items.sortBy(i, i.qty)`. The namespaced members
 * of the same libraries live in {@link EXT_NAMESPACES}.
 */
export const EXT_METHODS: CelEntry[] = [
  // --- strings ---
  {
    name: "upperAscii",
    kind: "function",
    signature: "string.upperAscii() -> string",
    summary: "Uppercase the ASCII letters in a string; other runes are unchanged.",
    example: "body.code.upperAscii()",
  },
  {
    name: "lowerAscii",
    kind: "function",
    signature: "string.lowerAscii() -> string",
    summary:
      "Lowercase the ASCII letters in a string — the usual way to normalize an email or a header value before comparing it.",
    example: "body.email.lowerAscii()",
  },
  {
    name: "trim",
    kind: "function",
    signature: "string.trim() -> string",
    summary: "Strip leading and trailing whitespace (the Unicode definition).",
    example: "body.reference.trim()",
  },
  {
    name: "replace",
    kind: "function",
    signature: "string.replace(old, new, limit?) -> string",
    summary:
      "Replace every occurrence of a substring; an optional limit caps how many, and a negative limit replaces all.",
    example: 'body.path.replace("/v1/", "/v2/")',
  },
  {
    name: "split",
    kind: "function",
    signature: "string.split(separator, limit?) -> list(string)",
    summary:
      "Split a string on a separator into a list; an optional limit caps the number of pieces.",
    example: 'body.tags.split(",")',
  },
  {
    name: "substring",
    kind: "function",
    signature: "string.substring(start, end?) -> string",
    summary:
      "The rune range from start (inclusive) to end (exclusive); omit end for the rest of the string.",
    example: "body.id.substring(0, 8)",
  },
  {
    name: "charAt",
    kind: "function",
    signature: "string.charAt(index) -> string",
    summary: "The single character at a zero-based index (empty at the string's length).",
    example: "body.code.charAt(0)",
  },
  {
    name: "indexOf",
    kind: "function",
    signature: "string.indexOf(substring, offset?) -> int",
    summary: "The index of the first occurrence of a substring, or -1 when absent.",
    example: 'body.subject.indexOf(":")',
  },
  {
    name: "lastIndexOf",
    kind: "function",
    signature: "string.lastIndexOf(substring, offset?) -> int",
    summary: "The index of the last occurrence of a substring, or -1 when absent.",
    example: 'body.path.lastIndexOf("/")',
  },
  {
    name: "join",
    kind: "function",
    signature: "list(string).join(separator?) -> string",
    summary:
      "Concatenate a list of strings, optionally separated. The inverse of split, and the tidy way to render a list into a message.",
    example: 'body.roles.join(", ")',
  },
  {
    name: "format",
    kind: "function",
    signature: "string.format(list) -> string",
    summary:
      "Interpolate a format string with a list of arguments — %s, %d, %f, %.2f, %e, %b, %o, %x/%X. Interpolation without reaching for a template.",
    example: '"order %s: %d lines".format([body.id, size(body.lines)])',
  },
  {
    name: "reverse",
    kind: "function",
    signature: "string.reverse() -> string | list.reverse() -> list",
    summary: "Reverse a string (by rune) or a list.",
    example: "body.items.reverse()",
  },
  // --- lists ---
  {
    name: "distinct",
    kind: "function",
    signature: "list.distinct() -> list",
    summary:
      "Drop duplicate elements, keeping first-seen order. Works on any comparable element, including maps and lists.",
    example: "body.tags.distinct()",
  },
  {
    name: "flatten",
    kind: "function",
    signature: "list.flatten(depth?) -> list",
    summary:
      "Flatten nested lists one level, or `depth` levels when given — e.g. to merge the per-page arrays of a paged response.",
    example: "body.pages.map(p, p.items).flatten()",
  },
  {
    name: "slice",
    kind: "function",
    signature: "list.slice(start, end) -> list",
    summary: "The sub-list from start (inclusive) to end (exclusive).",
    example: "body.items.slice(0, 10)",
  },
  {
    name: "sort",
    kind: "function",
    signature: "list.sort() -> list",
    summary:
      "Sort a list of comparable scalars (int, uint, double, bool, duration, timestamp, string, bytes) ascending.",
    example: "body.tags.sort()",
  },
  {
    name: "sortBy",
    kind: "function",
    signature: "list.sortBy(x, keyExpr) -> list",
    summary:
      "Sort by a computed key, ascending — the ordering operation with no standard-CEL workaround.",
    example: "body.items.sortBy(i, i.price)",
  },
  {
    name: "first",
    kind: "function",
    signature: "list.first() -> optional(T)",
    summary:
      "The first element as an optional, so an empty list is absent rather than an out-of-range error. Pair with orValue.",
    example: 'body.items.first().orValue("none")',
  },
  {
    name: "last",
    kind: "function",
    signature: "list.last() -> optional(T)",
    summary: "The last element as an optional; empty lists yield absent.",
    example: "body.events.last().orValue(null)",
  },
  // --- two-variable comprehensions ---
  {
    name: "transformList",
    kind: "function",
    signature: "(list|map).transformList(k, v, filter?, expr) -> list",
    summary:
      "Build a list with both the index/key and the value in scope, optionally filtering. The two-variable map().",
    example: "body.items.transformList(i, v, string(i) + v.sku)",
  },
  {
    name: "transformMap",
    kind: "function",
    signature: "(list|map).transformMap(k, v, filter?, expr) -> map",
    summary:
      "Rewrite the values of a map (or index a list) keeping the keys, with key and value both in scope.",
    example: "body.headers.transformMap(k, v, v.lowerAscii())",
  },
  {
    name: "transformMapEntry",
    kind: "function",
    signature: "(list|map).transformMapEntry(k, v, filter?, entryExpr) -> map",
    summary:
      "Build a map from each entry, choosing both key and value — the way to turn a list into a lookup map without a foreach. An empty map result skips the entry.",
    example: "body.lines.transformMapEntry(i, l, {l.sku: l.qty})",
  },
  {
    name: "existsOne",
    kind: "function",
    signature: "(list|map).existsOne(k, v, predicate) -> bool",
    summary:
      "True when exactly one entry satisfies the predicate, with key and value both in scope (the two-variable exists_one).",
    example: "body.items.existsOne(i, v, v.primary)",
  },
  // --- optional (enabled for the regex library, useful on its own) ---
  {
    name: "orValue",
    kind: "function",
    signature: "optional(T).orValue(fallback) -> T",
    summary:
      "The optional's value, or the fallback when it is absent — how a regex.extract miss or an empty-list first() gets a default.",
    example: 'regex.extract(body.ref, "id:(\\\\d+)").orValue("unknown")',
  },
  {
    name: "hasValue",
    kind: "function",
    signature: "optional(T).hasValue() -> bool",
    summary: "True when the optional holds a value. Test before branching on a match.",
    example: 'regex.extract(body.ref, "id:(\\\\d+)").hasValue()',
  },
];

/**
 * The namespaced function families the extension libraries add. Keyed by the
 * namespace an author types before the dot; each entry's `name` is the member, so
 * accepting a completion after `math.` inserts just `greatest(`.
 */
export const EXT_NAMESPACES: Record<string, CelEntry[]> = {
  base64: [
    {
      name: "encode",
      kind: "function",
      signature: "base64.encode(bytes) -> string",
      summary:
        "Standard base64-encode a byte string — basic-auth headers, binary payloads, opaque idempotency keys.",
      example: "base64.encode(bytes(body.token))",
    },
    {
      name: "decode",
      kind: "function",
      signature: "base64.decode(string) -> bytes",
      summary:
        "Decode standard base64 into bytes; wrap in string(...) to read it as text.",
      example: "string(base64.decode(body.payload))",
    },
  ],
  lists: [
    {
      name: "range",
      kind: "function",
      signature: "lists.range(n) -> list(int)",
      summary:
        "The integers 0 to n-1 — a counted sequence to map over when there is no list to iterate.",
      example: "lists.range(3)",
    },
  ],
  math: [
    {
      name: "greatest",
      kind: "function",
      signature: "math.greatest(list) | math.greatest(a, b, …) -> number",
      summary:
        "The largest of a list or argument set. With map() this is how a maximum is taken over a collection.",
      example: "math.greatest(body.items.map(i, i.price))",
    },
    {
      name: "least",
      kind: "function",
      signature: "math.least(list) | math.least(a, b, …) -> number",
      summary: "The smallest of a list or argument set.",
      example: "math.least(body.bids.map(b, b.amount))",
    },
    {
      name: "ceil",
      kind: "function",
      signature: "math.ceil(double) -> double",
      summary: "Round up to the nearest whole number.",
      example: "math.ceil(body.total)",
    },
    {
      name: "floor",
      kind: "function",
      signature: "math.floor(double) -> double",
      summary: "Round down to the nearest whole number.",
      example: "math.floor(body.total)",
    },
    {
      name: "round",
      kind: "function",
      signature: "math.round(double) -> double",
      summary:
        "Round to the nearest whole number, ties away from zero. Not for money: a JSON number is a binary double, so math.round(1.005 * 100.0) / 100.0 is 1.00, a cent low. Carry money in integer minor units (cents) instead.",
      example: "math.round(body.score)",
    },
    {
      name: "trunc",
      kind: "function",
      signature: "math.trunc(double) -> double",
      summary: "Drop the fractional part, toward zero.",
      example: "math.trunc(body.rate)",
    },
    {
      name: "abs",
      kind: "function",
      signature: "math.abs(number) -> number",
      summary: "Absolute value.",
      example: "math.abs(body.delta)",
    },
    {
      name: "sign",
      kind: "function",
      signature: "math.sign(number) -> number",
      summary: "-1, 0, or 1 according to the sign of the value.",
      example: "math.sign(body.delta)",
    },
    {
      name: "sqrt",
      kind: "function",
      signature: "math.sqrt(number) -> double",
      summary: "Square root; a negative input yields NaN.",
      example: "math.sqrt(body.area)",
    },
    {
      name: "isNaN",
      kind: "function",
      signature: "math.isNaN(double) -> bool",
      summary: "True when the value is NaN.",
      example: "math.isNaN(body.score)",
    },
    {
      name: "isInf",
      kind: "function",
      signature: "math.isInf(double) -> bool",
      summary: "True when the value is positive or negative infinity.",
      example: "math.isInf(body.ratio)",
    },
    {
      name: "isFinite",
      kind: "function",
      signature: "math.isFinite(double) -> bool",
      summary:
        "True when the value is neither NaN nor infinite — the guard to apply before arithmetic on a number that came out of JSON.",
      example: "math.isFinite(body.amount)",
    },
    {
      name: "bitAnd",
      kind: "function",
      signature: "math.bitAnd(int, int) -> int",
      summary: "Bitwise AND of two ints (or two uints).",
      example: "math.bitAnd(body.flags, 4)",
    },
    {
      name: "bitOr",
      kind: "function",
      signature: "math.bitOr(int, int) -> int",
      summary: "Bitwise OR of two ints (or two uints).",
      example: "math.bitOr(body.flags, 1)",
    },
    {
      name: "bitXor",
      kind: "function",
      signature: "math.bitXor(int, int) -> int",
      summary: "Bitwise XOR of two ints (or two uints).",
      example: "math.bitXor(body.flags, 3)",
    },
    {
      name: "bitNot",
      kind: "function",
      signature: "math.bitNot(int) -> int",
      summary: "Bitwise complement.",
      example: "math.bitNot(body.mask)",
    },
    {
      name: "bitShiftLeft",
      kind: "function",
      signature: "math.bitShiftLeft(int, int) -> int",
      summary: "Shift left by n bits; a negative shift is an error.",
      example: "math.bitShiftLeft(1, 4)",
    },
    {
      name: "bitShiftRight",
      kind: "function",
      signature: "math.bitShiftRight(int, int) -> int",
      summary: "Shift right by n bits (logical, not sign-extending).",
      example: "math.bitShiftRight(body.flags, 2)",
    },
  ],
  optional: [
    {
      name: "of",
      kind: "function",
      signature: "optional.of(value) -> optional(T)",
      summary: "Wrap a value as a present optional.",
      example: "optional.of(body.id)",
    },
    {
      name: "ofNonZeroValue",
      kind: "function",
      signature: "optional.ofNonZeroValue(value) -> optional(T)",
      summary:
        "Present when the value is non-zero (non-empty string/list/map, non-zero number), absent otherwise.",
      example: "optional.ofNonZeroValue(body.note).orValue(null)",
    },
    {
      name: "none",
      kind: "function",
      signature: "optional.none() -> optional(T)",
      summary: "The absent optional. Evaluates to null when it is an expression's result.",
      example: "optional.none()",
    },
    {
      name: "unwrap",
      kind: "function",
      signature: "optional.unwrap(list(optional(T))) -> list(T)",
      summary: "Drop the absent entries from a list of optionals.",
      example:
        'optional.unwrap(body.refs.map(r, regex.extract(r, "id:(\\\\d+)")))',
    },
  ],
  regex: [
    {
      name: "replace",
      kind: "function",
      signature: "regex.replace(target, pattern, replacement, count?) -> string",
      summary:
        "Replace every match of an RE2 pattern; \\1…\\9 in the replacement insert capture groups, and count caps the replacements.",
      example: 'regex.replace(body.text, "\\\\s+", " ")',
    },
    {
      name: "extract",
      kind: "function",
      signature: "regex.extract(target, pattern) -> optional(string)",
      summary:
        "The first match (or its single capture group) as an optional — absent when the pattern does not match. More than one capture group is an error.",
      example: 'regex.extract(body.ref, "ORDER-(\\\\d+)").orValue("")',
    },
    {
      name: "extractAll",
      kind: "function",
      signature: "regex.extractAll(target, pattern) -> list(string)",
      summary:
        "Every match (or capture group) as a list; no matches yields an empty list.",
      example: 'regex.extractAll(body.text, "#(\\\\w+)")',
    },
  ],
  sets: [
    {
      name: "contains",
      kind: "function",
      signature: "sets.contains(list, sublist) -> bool",
      summary:
        "True when every element of the second list appears in the first — a scope/role check without a comprehension.",
      example: 'sets.contains(body.scopes, ["orders:write"])',
    },
    {
      name: "equivalent",
      kind: "function",
      signature: "sets.equivalent(list, list) -> bool",
      summary: "True when both lists hold the same elements, ignoring order and repeats.",
      example: "sets.equivalent(body.tags, vars.expectedTags)",
    },
    {
      name: "intersects",
      kind: "function",
      signature: "sets.intersects(list, list) -> bool",
      summary: "True when the lists share at least one element. Empty lists never intersect.",
      example: 'sets.intersects(body.roles, ["admin", "owner"])',
    },
  ],
  strings: [
    {
      name: "quote",
      kind: "function",
      signature: "strings.quote(string) -> string",
      summary:
        "Wrap a string in double quotes, escaping what needs escaping — for embedding a value in generated text.",
      example: "strings.quote(body.name)",
    },
  ],
};

/**
 * The namespace prefixes themselves, offered as top-level completions so `math` is
 * discoverable before the dot. Accepting one inserts `math.`, which immediately
 * offers its members.
 */
export const EXT_NAMESPACE_ROOTS: CelEntry[] = Object.keys(EXT_NAMESPACES).map(
  (ns) => ({
    name: ns,
    kind: "namespace" as const,
    signature: `${ns}.…`,
    summary: `${EXT_NAMESPACES[ns].length} function(s): ${EXT_NAMESPACES[ns]
      .map((e) => e.name)
      .join(", ")}.`,
    example: `${ns}.${EXT_NAMESPACES[ns][0].name}(…)`,
  }),
);

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
    signature: "(list|map).all(x, predicate) | .all(k, v, predicate) -> bool",
    summary:
      "Macro: true when the predicate holds for every element. The three-argument form puts the index/key and the value both in scope.",
    example: "body.items.all(x, x.qty > 0)",
  },
  {
    name: "exists",
    kind: "function",
    signature: "(list|map).exists(x, predicate) | .exists(k, v, predicate) -> bool",
    summary:
      "Macro: true when the predicate holds for at least one element. The three-argument form puts the index/key and the value both in scope.",
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

/** Every top-level completion, variables first then functions and namespaces. */
export function allCompletions(): CelEntry[] {
  return [
    ...CEL_VARIABLES,
    ...OCTO_FUNCTIONS,
    ...EXT_METHODS,
    ...CEL_BUILTINS,
    ...EXT_NAMESPACE_ROOTS,
  ];
}

/** The members of a namespace (`math` → its functions), or undefined if unknown. */
export function namespaceMembers(name: string): CelEntry[] | undefined {
  if (!Object.prototype.hasOwnProperty.call(EXT_NAMESPACES, name)) {
    return undefined;
  }
  return EXT_NAMESPACES[name];
}

/**
 * Look up one entry by exact name; undefined when unknown. A dotted name resolves
 * through its namespace, so hover docs work for `math.round` as well as `round`.
 */
export function lookup(name: string): CelEntry | undefined {
  const dot = name.indexOf(".");
  if (dot > 0) {
    const members = namespaceMembers(name.slice(0, dot));
    return members?.find((e) => e.name === name.slice(dot + 1));
  }
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
  "existsOne",
  "map",
  "filter",
  "size",
  "distinct",
  "flatten",
  "first",
  "last",
  "join",
  "reverse",
  "slice",
  "sort",
  "sortBy",
  "transformList",
  "transformMap",
  "transformMapEntry",
]);

/** Receiver-style methods for a string value (e.g. `path.startsWith(...)`). */
export const STRING_METHODS: CelEntry[] = pick([
  "startsWith",
  "endsWith",
  "contains",
  "matches",
  "size",
  "charAt",
  "format",
  "indexOf",
  "lastIndexOf",
  "lowerAscii",
  "replace",
  "reverse",
  "split",
  "substring",
  "trim",
  "upperAscii",
]);

/**
 * Receiver-style methods for a map value. The two-variable comprehensions are the
 * reason a map has any at all — standard CEL offers only `has()` and `size()` on
 * one.
 */
export const MAP_METHODS: CelEntry[] = pick([
  "all",
  "exists",
  "existsOne",
  "size",
  "transformList",
  "transformMap",
  "transformMapEntry",
]);
