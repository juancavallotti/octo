import type { EncodedShape, ObservedEntry, ObservedMessage } from "./types";

/**
 * Combining what one traced run saw with what earlier ones saw.
 *
 * Kept apart from the provider that stores it because it is a lattice, not a piece of
 * React: two beliefs about one value combine into the weaker of the two, and no
 * sequence of merges can make the editor more confident than its least confident
 * evidence. That is what makes accumulating across runs safe — a case that takes a
 * branch today does not erase what a case took yesterday, and a field that was a
 * string once and a number once comes back as neither.
 */

/** How many keys an object may carry before it is treated as a map. Matches the
 *  producer's cap in @octo/run-host's exec/shapes.ts. */
const MAX_KEYS = 24;

export function mergeShape(a: EncodedShape, b: EncodedShape): EncodedShape {
  if (a.t === "dyn" || b.t === "dyn") return { t: "dyn" };
  // Null alongside a shape is that shape; widening would cost every key it has to
  // say something this format cannot express anyway.
  if (a.t === "null") return b;
  if (b.t === "null") return a;
  if (a.t !== b.t) return { t: "dyn" };

  if (a.t === "list") {
    if (!a.of || !b.of) return { t: "list", ...(a.of || b.of ? { of: (a.of ?? b.of)! } : {}) };
    return { t: "list", of: mergeShape(a.of, b.of) };
  }
  if (a.t === "object") {
    const f: Record<string, EncodedShape> = {};
    for (const key of new Set([...Object.keys(a.f ?? {}), ...Object.keys(b.f ?? {})])) {
      const left = a.f?.[key];
      const right = b.f?.[key];
      f[key] = left && right ? mergeShape(left, right) : (left ?? right)!;
    }
    // A union that grew past the cap is a map, exactly as one sample that was already
    // that wide would have been.
    return Object.keys(f).length > MAX_KEYS ? { t: "object", f: {} } : { t: "object", f };
  }
  return a;
}

function mergeMessage(
  a: ObservedMessage | undefined,
  b: ObservedMessage | undefined,
): ObservedMessage | undefined {
  if (!a) return b;
  if (!b) return a;
  const pick = (x?: EncodedShape, y?: EncodedShape) => (x && y ? mergeShape(x, y) : (x ?? y));
  const body = pick(a.body, b.body);
  const vars = pick(a.vars, b.vars);
  return body || vars ? { ...(body ? { body } : {}), ...(vars ? { vars } : {}) } : undefined;
}

/** Fold `incoming` into `existing`, address by address. */
export function mergeObserved(
  existing: Record<string, ObservedEntry> | undefined,
  incoming: Record<string, ObservedEntry>,
): Record<string, ObservedEntry> {
  const out: Record<string, ObservedEntry> = { ...(existing ?? {}) };
  for (const [address, entry] of Object.entries(incoming)) {
    const before = out[address];
    const received = mergeMessage(before?.in, entry.in);
    const produced = mergeMessage(before?.out, entry.out);
    out[address] = {
      ...(received ? { in: received } : {}),
      ...(produced ? { out: produced } : {}),
    };
  }
  return out;
}

/**
 * Drop what is no longer addressable.
 *
 * An entry is keyed by a block's natural address, which a rename of the block changes
 * — meta/rename.ts re-roots a FLOW rename, but nothing follows a block. Without this
 * the file accumulates entries for blocks that no longer exist, for as long as the
 * project does.
 */
export function pruneObserved(
  observed: Record<string, ObservedEntry> | undefined,
  live: Iterable<string>,
): Record<string, ObservedEntry> | undefined {
  if (!observed) return undefined;
  const keep = new Set(live);
  const out = Object.fromEntries(Object.entries(observed).filter(([address]) => keep.has(address)));
  return Object.keys(out).length > 0 ? out : undefined;
}

/**
 * The flow name an address is rooted at.
 *
 * An address is `<flow>.<block>…`, and a flow's error chain is `<flow>[error].…` —
 * so the root ends at whichever of `.` or `[` comes first. Returns null for anything
 * that is not shaped like an address at all.
 */
export function flowOfAddress(address: string): string | null {
  const end = Math.min(
    ...[address.indexOf("."), address.indexOf("[")].filter((i) => i >= 0),
    address.length,
  );
  const name = address.slice(0, end);
  return name === "" ? null : name;
}

/**
 * Split learned shapes by the flow they belong to.
 *
 * A run of one flow's suite can reach another flow through a flow-ref, and the
 * addresses it reports are rooted at whichever flow the block is actually in. Filing
 * them all under the flow that was run would be tidy and wrong: a rename of that flow
 * would re-root addresses belonging to a different one.
 */
export function byFlow(
  shapes: Record<string, ObservedEntry>,
): Map<string, Record<string, ObservedEntry>> {
  const out = new Map<string, Record<string, ObservedEntry>>();
  for (const [address, entry] of Object.entries(shapes)) {
    const flow = flowOfAddress(address);
    if (!flow) continue;
    out.set(flow, { ...(out.get(flow) ?? {}), [address]: entry });
  }
  return out;
}
