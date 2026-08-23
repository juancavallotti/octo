# Expressions

Every value documented as an *expression* is CEL, compiled once at build time and
evaluated per message. A bad expression fails the load, not the request.

## What is in scope

| Variable | Type | What it holds |
| --- | --- | --- |
| `body` | dyn | The decoded body — JSON-native shapes. In raw-content mode, a `{contentType, rawData}` envelope. |
| `vars` | map(string, dyn) | Message variables: path params, captured headers, the query map, and anything `set-variable` or `enrich` wrote. |
| `eventID` | string | The message's unique id. |
| `correlationID` | string | Set by a source or block; for following a message across flows. |
| `env` | map(string, string) | The declared environment variables, e.g. `env.API_BASE_URL`. An unresolved name is an evaluation error. |
| `now` | timestamp | Evaluation time. `string(now)`, or `now - duration("1h")`. |

A **source payload** expression runs before any message exists, so it sees only
`now` and `settings`. The functions below are still available there.

## Octo's own functions

| Function | Signature | Use |
| --- | --- | --- |
| `toJson` | `toJson(dyn) -> string` | Embed a structure into a string field — a log line, a prompt, an outgoing body. |
| `fromJson` | `fromJson(string) -> dyn` | Parse a JSON string into a value. |
| `toFormData` | `toFormData(map) -> string` | URL-encoded form body. |
| `fromFormData` | `fromFormData(string) -> map` | Parse one. |
| `multipart` | `multipart() -> map` | Start an empty multipart parts map. |
| `addPart` | `parts.addPart(string, dyn) -> map` | Return a new parts map with one part added; never modifies its receiver. A scalar is a text field; an object may set `data`, `encoding`, `filename`, `contentType`. |
| `fromMultipart` | `fromMultipart(body) -> map`, or `fromMultipart(rawData, contentType) -> map` | Decode a multipart payload into parts by name. The two-argument form is for a payload whose content type is not on the body — carried in a variable, say. The http source already does this into `body.parts`; use this for multipart off a queue, a file, or a `rest` response. |
| `toMultipart` | `toMultipart(map[, boundary]) -> string` | Render a parts map as a multipart body. The `rest` block does this itself with `bodyType: multipart`. |
| `toYaml` | `toYaml(dyn) -> string` | Render a value as a YAML document. Ambiguous strings (`y`, `no`, `1.0`) are quoted for you. |
| `fromYaml` | `fromYaml(string) -> dyn` | Parse a YAML document, normalized to JSON-native shapes (ints become numbers, timestamps become RFC 3339 strings). |
| `toEnv` | `toEnv(map) -> string` | Render a flat map as `.env` content, keys sorted. A nested value is an error. |
| `fromEnv` | `fromEnv(string) -> map` | Parse `.env` content. Every value is a string. |
| `templateResource` | `templateResource(name) -> string` | Render a declared template resource against the current message. |
| `hmacSha256` | `hmacSha256(dyn, dyn) -> bytes` | HMAC-SHA256 of a payload under a key. Render with `hexEncode` or `base64.encode`. |
| `hmacSha1` | `hmacSha1(dyn, dyn) -> bytes` | The same with SHA-1, for legacy schemes only. |
| `hexEncode` | `hexEncode(dyn) -> string` | Render bytes as lowercase hex. |
| `secureCompare` | `secureCompare(dyn, dyn) -> bool` | Constant-time compare. Always use it for signatures — `==` leaks the match length via timing. |
| `uuid` | `uuid() -> string` | A random v4 UUID: correlation ids, idempotency keys, a synthetic id for a record without one. Non-deterministic like `now`, so a trace replay does not reproduce it — and never for `memoryThreadId`, which is evaluated once per run, so a minted thread saves a transcript nobody will read. |

## Standard libraries available

Strings (including `format`), lists, encoders (base64), math, two-variable
comprehensions, sets, and regex. Regex functions return `optional<T>` — a miss is a
successful "no match", not an error — so unwrap with `orValue`.

## Idioms that come up constantly

```cel
// Guard an optional field. has() is the difference between a default and a crash.
has(body.query) ? body.query : {}

// Build a JSON body.
'{"name": body.name, "at": string(now)}'

// A CEL *string literal* — note the inner quotes. A bare /orders is not valid CEL.
'"/orders"'

// Read a path param or captured header.
vars.id
vars["X-Request-Id"]

// Filter by method on an HTTP route, which accepts every method.
vars.method == "DELETE"

// Environment variable inside an expression — prefer this over ${NAME},
// which lands as raw CEL source.
env.API_BASE_URL
```

The most common mistake in a static-vs-expression setting: `path: /orders` is a
plain string setting on `rest`, but `path: '"/orders"'` on `rest-dynamic`, where the
setting is an expression. Read the block's table before writing the value.

## Checking one

`bin/octo eval` evaluates an expression against a body without running anything:

```bash
bin/octo eval --expr '"hi " + body.name' --data '{"name": "Ada"}'
# {"ok":true,"result":"hi Ada"}
```

Prefer proving an expression this way over asserting it works.
