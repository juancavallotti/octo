/**
 * A hand-authored catalogue of the CEL variables and functions available to Octo
 * message expressions (log messages, payloads, if/switch conditions, connector
 * fields, …), served by the `getCelFunctions` tool.
 *
 * SOURCE OF TRUTH — these live only as Go declarations in the runtime's single CEL
 * seam; there is no machine-readable export to transcribe from, so this table is
 * maintained by hand. When a `RegisterMessageExtension` is added/changed in Go,
 * update this file to match:
 *   - variables: runtime/core/expr/message.go (MessageVars)
 *   - toJson/fromJson: runtime/core/expr/json.go
 *   - toFormData/fromFormData: runtime/core/expr/formdata.go
 *   - templateResource: runtime/core/expr/template.go
 *   - hmacSha256/hmacSha1/hexEncode/secureCompare: runtime/core/expr/crypto.go
 *   - the cel-go extension libraries and their pinned versions, which decide
 *     which of their functions exist: runtime/core/expr/stdext.go
 */

/** A variable in scope for every message CEL expression. */
export interface CelVariable {
  name: string;
  /** The CEL type it evaluates to. */
  type: string;
  summary: string;
}

/**
 * Which library a function comes from. `octo` are the runtime's own; the rest are
 * cel-go extension libraries the runtime enables. The CEL standard library (size,
 * has, map, filter, string conversions, …) is always available and not listed
 * here — see the language reference for it.
 */
export type CelLibrary =
  | "octo"
  | "strings"
  | "lists"
  | "encoders"
  | "math"
  | "comprehensions"
  | "sets"
  | "regex"
  | "optional";

/** A CEL function available to message expressions. */
export interface CelFunction {
  name: string;
  library: CelLibrary;
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

/**
 * Every function available on top of the CEL standard library: the runtime's own
 * nine, then the cel-go extension libraries it enables. Receiver-style entries are
 * named bare (`upperAscii`) with the receiver shown in the signature; namespaced
 * entries carry their namespace (`math.round`).
 */
export const CEL_FUNCTIONS: CelFunction[] = [
  {
    name: "toJson",
    library: "octo",
    signature: "toJson(dyn) -> string",
    summary: "Marshal any value to a compact JSON string.",
    example: `toJson(body)`,
  },
  {
    name: "fromJson",
    library: "octo",
    signature: "fromJson(string) -> dyn",
    summary:
      "Parse a JSON string back into a decoded value — e.g. revert a captured raw JSON body.",
    example: `fromJson(body.rawData)`,
  },
  {
    name: "toFormData",
    library: "octo",
    signature: "toFormData(dyn) -> string",
    summary:
      "Encode an object as an application/x-www-form-urlencoded string (array fields emit repeated keys). Only urlencoded, not multipart.",
    example: `toFormData({"q": "hello", "page": 2})`,
  },
  {
    name: "fromFormData",
    library: "octo",
    signature: "fromFormData(string) -> dyn",
    summary:
      "Parse an application/x-www-form-urlencoded string into an object (repeated keys become a list) — e.g. decode a raw form POST body.",
    example: `fromFormData(body.rawData)`,
  },
  {
    name: "templateResource",
    library: "octo",
    signature: "templateResource(string) -> string",
    summary:
      "Render a template resource (by id) against the current message; the template sees the in-scope variables (body, vars, env, …) via {{ env.NAME }} / {{ body.* }}.",
    example: `templateResource("welcome-email")`,
  },

  {
    name: "hmacSha256",
    library: "octo",
    signature: "hmacSha256(dyn, dyn) -> bytes",
    summary:
      "The HMAC-SHA256 of a payload under a key, as raw bytes; both arguments take a string or bytes. Rendering is separate (hexEncode or base64.encode), so the same function serves any webhook scheme. Compare the result with secureCompare, never ==.",
    example: `"X-Hub-Signature-256" in vars && secureCompare(vars["X-Hub-Signature-256"], "sha256=" + hexEncode(hmacSha256(env.GITHUB_WEBHOOK_SECRET, vars.rawBody)))`,
  },
  {
    name: "hmacSha1",
    library: "octo",
    signature: "hmacSha1(dyn, dyn) -> bytes",
    summary:
      "The HMAC-SHA1 of a payload under a key. For legacy schemes that still sign with SHA-1 (Twilio, GitHub's older X-Hub-Signature) — do not choose it for anything new.",
    example: `hexEncode(hmacSha1(env.WEBHOOK_SECRET, vars.rawBody))`,
  },
  {
    name: "hexEncode",
    library: "octo",
    signature: "hexEncode(dyn) -> string",
    summary:
      "Render bytes as lowercase hex, the form most providers put a signature in. For base64 schemes (Shopify, Twilio) use base64.encode instead.",
    example: `hexEncode(hmacSha256(env.WEBHOOK_SECRET, vars.rawBody))`,
  },
  {
    name: "secureCompare",
    library: "octo",
    signature: "secureCompare(dyn, dyn) -> bool",
    summary:
      "Compare two values without a content-dependent early return. Always use this for a signature: == stops at the first differing byte, and the timing of that is observable, which over enough requests leaks the expected value a byte at a time. Guard the header read with `\"Name\" in vars &&` — an absent variable is an evaluation error, which fails the flow and answers 500 rather than rejecting.",
    example: `"X-Signature" in vars && secureCompare(vars["X-Signature"], hexEncode(hmacSha256(env.WEBHOOK_SECRET, vars.rawBody)))`,
  },

  // --- strings ---
  {
    name: "upperAscii",
    library: "strings",
    signature: "string.upperAscii() -> string",
    summary: "Uppercase the ASCII letters in a string.",
    example: `body.code.upperAscii()`,
  },
  {
    name: "lowerAscii",
    library: "strings",
    signature: "string.lowerAscii() -> string",
    summary:
      "Lowercase the ASCII letters in a string — normalize an email or header before comparing.",
    example: `body.email.lowerAscii()`,
  },
  {
    name: "trim",
    library: "strings",
    signature: "string.trim() -> string",
    summary: "Strip leading and trailing whitespace.",
    example: `body.reference.trim()`,
  },
  {
    name: "replace",
    library: "strings",
    signature: "string.replace(old, new, limit?) -> string",
    summary:
      "Replace occurrences of a substring; an optional limit caps how many (negative replaces all).",
    example: `body.path.replace("/v1/", "/v2/")`,
  },
  {
    name: "split",
    library: "strings",
    signature: "string.split(separator, limit?) -> list(string)",
    summary: "Split a string on a separator; an optional limit caps the pieces.",
    example: `body.tags.split(",")`,
  },
  {
    name: "join",
    library: "strings",
    signature: "list(string).join(separator?) -> string",
    summary: "Concatenate a list of strings, optionally separated. The inverse of split.",
    example: `body.roles.join(", ")`,
  },
  {
    name: "substring",
    library: "strings",
    signature: "string.substring(start, end?) -> string",
    summary: "The rune range from start (inclusive) to end (exclusive).",
    example: `body.id.substring(0, 8)`,
  },
  {
    name: "charAt",
    library: "strings",
    signature: "string.charAt(index) -> string",
    summary: "The single character at a zero-based index.",
    example: `body.code.charAt(0)`,
  },
  {
    name: "indexOf",
    library: "strings",
    signature: "string.indexOf(substring, offset?) -> int",
    summary: "Index of the first occurrence, or -1.",
    example: `body.subject.indexOf(":")`,
  },
  {
    name: "lastIndexOf",
    library: "strings",
    signature: "string.lastIndexOf(substring, offset?) -> int",
    summary: "Index of the last occurrence, or -1.",
    example: `body.path.lastIndexOf("/")`,
  },
  // cel-go declares reverse twice — once on string in the strings library, once
  // on list in the lists library — so the catalogue carries it twice too. A
  // single entry would leave whichever library it was not filed under looking
  // like it has no reverse.
  {
    name: "reverse",
    library: "strings",
    signature: "string.reverse() -> string",
    summary: "Reverse a string by rune. Lists have their own reverse, in the lists library.",
    example: `body.code.reverse()`,
  },
  {
    name: "format",
    library: "strings",
    signature: "string.format(list) -> string",
    summary:
      "Interpolate a format string with a list of arguments (%s, %d, %f, %.2f, %e, %b, %o, %x/%X) — interpolation without a template.",
    example: `"order %s: %d lines".format([body.id, size(body.lines)])`,
  },
  {
    name: "strings.quote",
    library: "strings",
    signature: "strings.quote(string) -> string",
    summary: "Wrap a string in double quotes, escaping what needs escaping.",
    example: `strings.quote(body.name)`,
  },

  // --- lists ---
  {
    name: "distinct",
    library: "lists",
    signature: "list.distinct() -> list",
    summary: "Drop duplicate elements, keeping first-seen order.",
    example: `body.tags.distinct()`,
  },
  {
    name: "reverse",
    library: "lists",
    signature: "list.reverse() -> list",
    summary:
      "Reverse a list — compose with sort/sortBy for a descending order. Strings have their own reverse, in the strings library.",
    example: `body.items.sortBy(i, i.amount).reverse()`,
  },
  {
    name: "flatten",
    library: "lists",
    signature: "list.flatten(depth?) -> list",
    summary: "Flatten nested lists one level, or `depth` levels when given.",
    example: `body.pages.map(p, p.items).flatten()`,
  },
  {
    name: "slice",
    library: "lists",
    signature: "list.slice(start, end) -> list",
    summary: "The sub-list from start (inclusive) to end (exclusive).",
    example: `body.items.slice(0, 10)`,
  },
  {
    name: "sort",
    library: "lists",
    signature: "list.sort() -> list",
    summary: "Sort a list of comparable scalars ascending.",
    example: `body.tags.sort()`,
  },
  {
    name: "sortBy",
    library: "lists",
    signature: "list.sortBy(x, keyExpr) -> list",
    summary: "Sort by a computed key, ascending. No standard-CEL workaround exists.",
    example: `body.items.sortBy(i, i.price)`,
  },
  {
    name: "lists.range",
    library: "lists",
    signature: "lists.range(n) -> list(int)",
    summary: "The integers 0 to n-1 — a counted sequence to map over.",
    example: `lists.range(3)`,
  },

  // --- encoders ---
  {
    name: "base64.encode",
    library: "encoders",
    signature: "base64.encode(bytes) -> string",
    summary:
      "Standard base64-encode a byte string — basic-auth headers, binary payloads, opaque keys.",
    example: `base64.encode(bytes(env.USER + ":" + env.TOKEN))`,
  },
  {
    name: "base64.decode",
    library: "encoders",
    signature: "base64.decode(string) -> bytes",
    summary: "Decode standard base64 into bytes; wrap in string(...) to read as text.",
    example: `string(base64.decode(body.payload))`,
  },

  // --- math ---
  {
    name: "math.greatest",
    library: "math",
    signature: "math.greatest(list) | math.greatest(a, b, …) -> number",
    summary: "The largest of a list or argument set — a maximum over a mapped collection.",
    example: `math.greatest(body.items.map(i, i.price))`,
  },
  {
    name: "math.least",
    library: "math",
    signature: "math.least(list) | math.least(a, b, …) -> number",
    summary: "The smallest of a list or argument set.",
    example: `math.least(body.bids.map(b, b.amount))`,
  },
  {
    name: "math.round",
    library: "math",
    signature: "math.round(double) -> double",
    summary:
      "Round to the nearest whole number, ties away from zero. Not for money: a JSON number is a binary double, so math.round(1.005 * 100.0) / 100.0 is 1.00, a cent low. Carry money in integer minor units (cents) instead.",
    example: `math.round(body.score)`,
  },
  {
    name: "math.ceil",
    library: "math",
    signature: "math.ceil(double) -> double",
    summary: "Round up to the nearest whole number.",
    example: `math.ceil(body.total)`,
  },
  {
    name: "math.floor",
    library: "math",
    signature: "math.floor(double) -> double",
    summary: "Round down to the nearest whole number.",
    example: `math.floor(body.total)`,
  },
  {
    name: "math.trunc",
    library: "math",
    signature: "math.trunc(double) -> double",
    summary: "Drop the fractional part, toward zero.",
    example: `math.trunc(body.rate)`,
  },
  {
    name: "math.abs",
    library: "math",
    signature: "math.abs(number) -> number",
    summary: "Absolute value.",
    example: `math.abs(body.delta)`,
  },
  {
    name: "math.sign",
    library: "math",
    signature: "math.sign(number) -> number",
    summary: "-1, 0, or 1 according to the sign.",
    example: `math.sign(body.delta)`,
  },
  {
    name: "math.sqrt",
    library: "math",
    signature: "math.sqrt(number) -> double",
    summary: "Square root; a negative input yields NaN.",
    example: `math.sqrt(body.area)`,
  },
  {
    name: "math.isFinite",
    library: "math",
    signature: "math.isFinite(double) -> bool",
    summary:
      "True when the value is neither NaN nor infinite — the guard for a number that came out of JSON.",
    example: `math.isFinite(body.amount)`,
  },
  {
    name: "math.isNaN",
    library: "math",
    signature: "math.isNaN(double) -> bool",
    summary: "True when the value is NaN.",
    example: `math.isNaN(body.score)`,
  },
  {
    name: "math.isInf",
    library: "math",
    signature: "math.isInf(double) -> bool",
    summary: "True when the value is positive or negative infinity.",
    example: `math.isInf(body.ratio)`,
  },
  {
    name: "math.bitAnd",
    library: "math",
    signature: "math.bitAnd(int, int) -> int",
    summary:
      "Bitwise AND. bitOr, bitXor, bitNot, bitShiftLeft, and bitShiftRight round out the set.",
    example: `math.bitAnd(body.flags, 4)`,
  },
  {
    name: "math.bitOr",
    library: "math",
    signature: "math.bitOr(int, int) -> int",
    summary: "Bitwise OR.",
    example: `math.bitOr(body.flags, 2)`,
  },
  {
    name: "math.bitXor",
    library: "math",
    signature: "math.bitXor(int, int) -> int",
    summary: "Bitwise exclusive OR.",
    example: `math.bitXor(body.flags, 1)`,
  },
  {
    name: "math.bitNot",
    library: "math",
    signature: "math.bitNot(int) -> int",
    summary: "Bitwise NOT.",
    example: `math.bitNot(body.mask)`,
  },
  {
    name: "math.bitShiftLeft",
    library: "math",
    signature: "math.bitShiftLeft(int, int) -> int",
    summary: "Bitwise left shift.",
    example: `math.bitShiftLeft(body.flags, 1)`,
  },
  {
    name: "math.bitShiftRight",
    library: "math",
    signature: "math.bitShiftRight(int, int) -> int",
    summary: "Bitwise right shift.",
    example: `math.bitShiftRight(body.flags, 1)`,
  },

  // --- two-variable comprehensions ---
  {
    name: "transformList",
    library: "comprehensions",
    signature: "(list|map).transformList(k, v, filter?, expr) -> list",
    summary:
      "Build a list with both the index/key and the value in scope, optionally filtering. The two-variable map().",
    example: `body.items.transformList(i, v, string(i) + v.sku)`,
  },
  {
    name: "transformMap",
    library: "comprehensions",
    signature: "(list|map).transformMap(k, v, filter?, expr) -> map",
    summary: "Rewrite a map's values keeping its keys, with key and value both in scope.",
    example: `body.headers.transformMap(k, v, v.lowerAscii())`,
  },
  {
    name: "transformMapEntry",
    library: "comprehensions",
    signature: "(list|map).transformMapEntry(k, v, filter?, entryExpr) -> map",
    summary:
      "Build a map choosing both key and value — how a list becomes a lookup map without a foreach. An empty map result skips the entry.",
    example: `body.lines.transformMapEntry(i, l, {l.sku: l.qty})`,
  },
  {
    name: "existsOne",
    library: "comprehensions",
    signature: "(list|map).existsOne(k, v, predicate) -> bool",
    summary:
      "True when exactly one entry matches, with key and value in scope. `all` and `exists` take the same three-argument form.",
    example: `body.items.existsOne(i, v, v.primary)`,
  },

  // --- sets ---
  {
    name: "sets.contains",
    library: "sets",
    signature: "sets.contains(list, sublist) -> bool",
    summary:
      "True when every element of the second list appears in the first — a scope/role check without a comprehension.",
    example: `sets.contains(body.scopes, ["orders:write"])`,
  },
  {
    name: "sets.equivalent",
    library: "sets",
    signature: "sets.equivalent(list, list) -> bool",
    summary: "True when both lists hold the same elements, ignoring order and repeats.",
    example: `sets.equivalent(body.tags, vars.expectedTags)`,
  },
  {
    name: "sets.intersects",
    library: "sets",
    signature: "sets.intersects(list, list) -> bool",
    summary: "True when the lists share at least one element.",
    example: `sets.intersects(body.roles, ["admin", "owner"])`,
  },

  // --- regex ---
  {
    name: "regex.extract",
    library: "regex",
    signature: "regex.extract(target, pattern) -> optional(string)",
    summary:
      "The first match (or its single capture group) as an optional — absent when the pattern misses, which evaluates to null. More than one capture group is an error.",
    example: `regex.extract(body.ref, "ORDER-(\\\\d+)").orValue("")`,
  },
  {
    name: "regex.extractAll",
    library: "regex",
    signature: "regex.extractAll(target, pattern) -> list(string)",
    summary: "Every match (or capture group) as a list; no matches yields an empty list.",
    example: `regex.extractAll(body.text, "#(\\\\w+)")`,
  },
  {
    name: "regex.replace",
    library: "regex",
    signature: "regex.replace(target, pattern, replacement, count?) -> string",
    summary:
      "Replace every match of an RE2 pattern; \\1…\\9 insert capture groups and count caps the replacements.",
    example: `regex.replace(body.text, "\\\\s+", " ")`,
  },

  // --- optional (enabled for regex, useful on its own) ---
  {
    name: "orValue",
    library: "optional",
    signature: "optional(T).orValue(fallback) -> T",
    summary:
      "The optional's value, or the fallback when absent — how a regex.extract miss gets a default.",
    example: `regex.extract(body.ref, "id:(\\\\d+)").orValue("unknown")`,
  },
  {
    name: "hasValue",
    library: "optional",
    signature: "optional(T).hasValue() -> bool",
    summary: "True when the optional holds a value.",
    example: `regex.extract(body.ref, "id:(\\\\d+)").hasValue()`,
  },
  {
    name: "first",
    library: "optional",
    signature: "list.first() -> optional(T)",
    summary:
      "The first element as an optional, so an empty list is absent rather than an error. `last` is its mirror.",
    example: `body.items.first().orValue("none")`,
  },
  {
    name: "last",
    library: "optional",
    signature: "list.last() -> optional(T)",
    summary: "The last element as an optional, so an empty list is absent rather than an error.",
    example: `body.items.last().orValue("none")`,
  },
  {
    name: "optional.of",
    library: "optional",
    signature: "optional.of(value) -> optional(T)",
    summary:
      "Wrap a value as a present optional. optional.none() is the absent one, and optional.unwrap(list) drops the absent entries from a list.",
    example: `optional.of(body.id)`,
  },
  {
    name: "optional.none",
    library: "optional",
    signature: "optional.none() -> optional(T)",
    summary: "Construct an absent optional value.",
    example: `optional.none()`,
  },
  {
    name: "optional.unwrap",
    library: "optional",
    signature: "optional.unwrap(list(optional(T))) -> list(T)",
    summary: "Drop absent optionals from a list and unwrap the present values.",
    example: `optional.unwrap([optional.of(body.id), optional.none()])`,
  },
];

/**
 * Every entry with this name, in catalogue order; empty when unknown. A name can
 * appear more than once when two libraries declare it on different receivers —
 * `reverse` is a string function and a list function — and the caller wants both,
 * not whichever the catalogue happens to list first.
 */
export function getCelFunctionsNamed(name: string): CelFunction[] {
  return CEL_FUNCTIONS.filter((f) => f.name === name);
}

/** Every function from one library, in catalogue order. */
export function getCelLibrary(library: string): CelFunction[] {
  return CEL_FUNCTIONS.filter((f) => f.library === library);
}

/** The library names present in the catalogue, in catalogue order. */
export function celLibraries(): CelLibrary[] {
  return [...new Set(CEL_FUNCTIONS.map((f) => f.library))];
}
