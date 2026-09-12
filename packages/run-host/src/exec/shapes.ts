/**
 * Turning a trace into message *shapes* — keys and types, never values.
 *
 * A traced suite run is the best answer there is to "what does this flow's message
 * actually look like": the cases already say how to exercise the flow, and the mocks
 * they carry mean nothing real is called. What comes back is real data, though, and
 * the caller wants it in a file it commits. So the reduction happens here, on the
 * server, before the staged directory holding the traces is removed — and what leaves
 * this module carries no scalar from the run at all.
 *
 * That rule is structural rather than a list of fields to redact: {@link shapeOf}
 * cannot return a value because its return type has nowhere to put one. There is no
 * second place to get it wrong.
 */

/** A value's shape, as it travels to the caller and is stored. */
export interface Shape {
  t: "string" | "number" | "bool" | "null" | "list" | "object" | "dyn";
  /** Element shape, for a list. */
  of?: Shape;
  /** Field shapes, for an object. */
  f?: Record<string, Shape>;
}

/** The body and variables of one message. */
export interface MessageShapes {
  body?: Shape;
  vars?: Shape;
}

/** What was seen at one block: the message it received, and the one it produced. */
export interface ObservedShapes {
  in?: MessageShapes;
  out?: MessageShapes;
}

/**
 * How deep a shape is read. Past this nobody is completing a path by hand, and the
 * cost of going deeper is paid in a file somebody commits.
 */
const MAX_DEPTH = 5;
/** How many keys an object may carry before it is treated as a map — see below. */
const MAX_KEYS = 24;
/** How many elements of a list are merged to describe it. */
const LIST_SAMPLE = 10;

/**
 * Whether a key looks like data rather than a field name.
 *
 * This is the one place where capturing "only keys" still captures values: an object
 * keyed by email address, account id or order number has the user's data in its
 * keys. Such an object is not something anyone completes a path into anyway, so it
 * collapses to "a map, contents unknown" and the question goes away.
 */
function looksLikeData(key: string): boolean {
  return (
    key.includes("@") ||
    key.length > 64 ||
    /^\d+$/.test(key) ||
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(key) ||
    /^[0-9a-f]{24,}$/i.test(key)
  );
}

/** A map whose keys carry data, described as a map and nothing more. */
const OPAQUE_MAP: Shape = { t: "object", f: {} };

function objectShape(value: Record<string, unknown>, depth: number): Shape {
  const keys = Object.keys(value);
  const dataKeys = keys.filter(looksLikeData).length;
  // Two ways to be a map rather than a record: too many keys to be field names, or
  // enough of them that look like data to make the rest suspect.
  if (keys.length > MAX_KEYS || (keys.length > 0 && dataKeys / keys.length > 0.4)) {
    return OPAQUE_MAP;
  }
  const f: Record<string, Shape> = {};
  for (const key of keys) f[key] = shapeOf(value[key], depth + 1);
  return { t: "object", f };
}

/** The shape of one value. */
export function shapeOf(value: unknown, depth = 0): Shape {
  if (value === null) return { t: "null" };
  if (depth >= MAX_DEPTH) return { t: "dyn" };
  if (Array.isArray(value)) {
    const of = value
      .slice(0, LIST_SAMPLE)
      .reduce<Shape | undefined>((acc, item) => {
        const next = shapeOf(item, depth + 1);
        return acc ? mergeShape(acc, next) : next;
      }, undefined);
    return of ? { t: "list", of } : { t: "list" };
  }
  switch (typeof value) {
    case "string":
      return { t: "string" };
    case "number":
      return { t: "number" };
    case "boolean":
      return { t: "bool" };
    case "object":
      return objectShape(value as Record<string, unknown>, depth);
    default:
      return { t: "dyn" };
  }
}

/** The weaker of two beliefs about one value — merging only ever widens. */
export function mergeShape(a: Shape, b: Shape): Shape {
  if (a.t === "dyn" || b.t === "dyn") return { t: "dyn" };
  // Null alongside a shape is that shape; widening to dyn would cost every key it
  // has to say something this format cannot express anyway.
  if (a.t === "null") return b;
  if (b.t === "null") return a;
  if (a.t !== b.t) return { t: "dyn" };

  if (a.t === "list") {
    if (!a.of || !b.of) return { t: "list", of: a.of ?? b.of };
    return { t: "list", of: mergeShape(a.of, b.of) };
  }
  if (a.t === "object") {
    // Either side having collapsed to a map means the union would be one too.
    if (!a.f || !b.f || Object.keys(a.f).length === 0 || Object.keys(b.f).length === 0) {
      return Object.keys({ ...a.f, ...b.f }).length > MAX_KEYS ? OPAQUE_MAP : { t: "object", f: { ...a.f, ...b.f } };
    }
    const f: Record<string, Shape> = {};
    for (const key of new Set([...Object.keys(a.f), ...Object.keys(b.f)])) {
      const left = a.f[key];
      const right = b.f[key];
      f[key] = left && right ? mergeShape(left, right) : (left ?? right)!;
    }
    return Object.keys(f).length > MAX_KEYS ? OPAQUE_MAP : { t: "object", f };
  }
  return a;
}

function mergeMessages(a: MessageShapes | undefined, b: MessageShapes): MessageShapes {
  const pick = (x?: Shape, y?: Shape) => (x && y ? mergeShape(x, y) : (x ?? y));
  return { body: pick(a?.body, b.body), vars: pick(a?.vars, b.vars) };
}

/** One line of a trace file, as far as this module cares. */
interface TraceRecord {
  kind?: string;
  path?: string;
  body?: unknown;
  vars?: unknown;
}

/**
 * Fold trace records into what was seen at each block address.
 *
 * `block.pre-invoke` is the message a block received and `block.post-invoke` the one
 * it produced. They stay apart because a variable a block sets belongs downstream of
 * it: folding them together would offer that variable in the block's own settings,
 * where it does not exist yet.
 */
export function reduceTrace(
  records: readonly TraceRecord[],
  into: Map<string, ObservedShapes> = new Map(),
): Map<string, ObservedShapes> {
  for (const record of records) {
    const side = record.kind === "block.pre-invoke" ? "in" : record.kind === "block.post-invoke" ? "out" : null;
    if (!side || !record.path) continue;
    const seen: MessageShapes = {
      ...(record.body !== undefined ? { body: shapeOf(record.body) } : {}),
      ...(record.vars !== undefined ? { vars: shapeOf(record.vars) } : {}),
    };
    if (!seen.body && !seen.vars) continue;
    const existing = into.get(record.path) ?? {};
    into.set(record.path, { ...existing, [side]: mergeMessages(existing[side], seen) });
  }
  return into;
}

/** Parse a JSON Lines trace, skipping anything that does not parse. */
export function parseTrace(content: string): TraceRecord[] {
  const out: TraceRecord[] = [];
  for (const line of content.split("\n")) {
    if (line.trim() === "") continue;
    try {
      out.push(JSON.parse(line) as TraceRecord);
    } catch {
      // A trace file is appended to by a process that may have been killed, so a
      // torn last line is normal. Everything before it is still worth having.
    }
  }
  return out;
}

/** The plain-object form, for a JSON response. */
export function toRecord(shapes: Map<string, ObservedShapes>): Record<string, ObservedShapes> {
  return Object.fromEntries(shapes);
}
