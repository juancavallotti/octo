import type { Field, Origin, ValueShape } from "./types";

/**
 * The shape lattice: reading a shape out of a sample, widening two shapes into one,
 * and walking a dotted path through one.
 *
 * `merge` only ever widens, never narrows. That single property is what lets the
 * additive walk, the join after a switch's branches, and (later) merging samples
 * across several runs all be the same function: two beliefs about one value combine
 * into the weaker of the two, and no sequence of merges can make the editor more
 * confident than its least confident evidence.
 */

export const UNKNOWN: ValueShape = { kind: "unknown" };
export const DYN: ValueShape = { kind: "dyn" };

/** How deep a sample is read. Past this the answer stops being useful to complete. */
const MAX_DEPTH = 6;

export function field(shape: ValueShape, origin: Origin, note?: string, certain = true): Field {
  return { shape, origin, ...(note ? { note } : {}), certain };
}

/** An open object of named fields — the normal shape for a body or for `vars`. */
export function objectOf(fields: Record<string, Field>, open = true): ValueShape {
  return { kind: "object", fields, open };
}

/** The shape of a parsed JSON value. */
export function shapeOfJson(value: unknown, origin: Origin, note?: string, depth = 0): ValueShape {
  if (value === null) return { kind: "null" };
  if (depth >= MAX_DEPTH) return DYN;
  if (Array.isArray(value)) {
    // Every element merged rather than the first taken: a list of differently shaped
    // objects must complete to what they have in common, not to whichever came first.
    const of = value.reduce<ValueShape>(
      (acc, item) => merge(acc, shapeOfJson(item, origin, note, depth + 1)),
      UNKNOWN,
    );
    return { kind: "list", of };
  }
  switch (typeof value) {
    case "string":
      return { kind: "string" };
    case "number":
      return { kind: "number" };
    case "boolean":
      return { kind: "bool" };
    case "object": {
      const fields: Record<string, Field> = {};
      for (const [key, v] of Object.entries(value as Record<string, unknown>)) {
        fields[key] = field(shapeOfJson(v, origin, note, depth + 1), origin, note);
      }
      // Open: one sample says these keys can be there, never that others cannot.
      return objectOf(fields);
    }
    default:
      return DYN;
  }
}

/** The weaker of two beliefs about one value. */
export function merge(a: ValueShape, b: ValueShape): ValueShape {
  if (a.kind === "unknown") return b;
  if (b.kind === "unknown") return a;
  if (a.kind === "dyn" || b.kind === "dyn") return DYN;
  // `null` alongside anything is that thing, optional — which this model does not
  // express, so the shape survives and the nullness is dropped rather than widening
  // every nullable field to dyn and losing its keys.
  if (a.kind === "null") return b;
  if (b.kind === "null") return a;
  if (a.kind !== b.kind) return DYN;

  if (a.kind === "list" && b.kind === "list") return { kind: "list", of: merge(a.of, b.of) };
  if (a.kind === "object" && b.kind === "object") return mergeObjects(a, b);
  return a;
}

function mergeObjects(
  a: Extract<ValueShape, { kind: "object" }>,
  b: Extract<ValueShape, { kind: "object" }>,
): ValueShape {
  const fields: Record<string, Field> = {};
  for (const key of new Set([...Object.keys(a.fields), ...Object.keys(b.fields)])) {
    const left = a.fields[key];
    const right = b.fields[key];
    if (!left || !right) {
      // Present on one side only, so it is not there on every path.
      fields[key] = { ...(left ?? right)!, certain: false };
      continue;
    }
    fields[key] = mergeFields(left, right);
  }
  return objectOf(fields, a.open || b.open);
}

export function mergeFields(a: Field, b: Field): Field {
  return {
    shape: merge(a.shape, b.shape),
    // The weaker provenance wins, in the order declared > inferred > sample.
    origin: weaker(a.origin, b.origin),
    ...(a.note ?? b.note ? { note: a.note ?? b.note } : {}),
    certain: a.certain && b.certain,
  };
}

const STRENGTH: Record<Origin, number> = { declared: 3, inferred: 2, sample: 1 };

function weaker(a: Origin, b: Origin): Origin {
  return STRENGTH[a] <= STRENGTH[b] ? a : b;
}

/** Follow a dotted path into a shape. Undefined when it cannot be followed. */
export function shapeAtPath(shape: ValueShape, path: string[]): ValueShape | undefined {
  let at: ValueShape = shape;
  for (const segment of path) {
    if (at.kind !== "object") return undefined;
    const next = at.fields[segment];
    if (!next) return undefined;
    at = next.shape;
  }
  return at;
}

/** Follow a dotted path and return the object's fields there, if it is an object. */
export function fieldsAtPath(
  shape: ValueShape,
  path: string[],
): Record<string, Field> | undefined {
  const at = shapeAtPath(shape, path);
  return at?.kind === "object" ? at.fields : undefined;
}
