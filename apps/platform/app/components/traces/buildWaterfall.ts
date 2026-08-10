/**
 * Turning one trace's records into the tree a waterfall is drawn from.
 *
 * The runtime's trace stream has no span ids and no parent pointers. What it has
 * instead is enough to reconstruct both: a record is stamped when the thing it
 * describes *finished* and carries how long that took, so every span is exactly
 * `[ts - durationNs, ts]`; a block's address says where it sits; and an event id
 * is re-minted at each sub-invocation boundary, so it groups an invocation.
 *
 * This module assembles those three. The address decides nesting where it can
 * (`./nesting`), the clock decides it where the address cannot, and the parts
 * that genuinely cannot be known — which input became which outcome in a fork, an
 * invocation whose terminal was lost — are reported as unknown rather than
 * guessed. Everything here is pure, so re-zooming and re-filtering never refetch.
 *
 * **One thing this never does** is treat a gap in `seq` as lost records. `seq` is
 * gapless across a whole *publisher*, not within a trace, so one trace's records
 * are normally non-consecutive. Loss is reported per app from the `trace.dropped`
 * marker, which carries no trace id and so belongs to no trace at all.
 */

import type { TraceRecord } from "@/app/model/traces";
import { attachInputs, type PreInvoke } from "./blockInputs";
import { blockLabel } from "./blockPath";
import { nest } from "./nesting";
import {
  compareInstants,
  minusNs,
  nsBetween,
  parseInstant,
  type Instant,
} from "./timeSpans";
import {
  addressKey,
  SPAN_KINDS,
  type Waterfall,
  type WaterfallNode,
  type WaterfallWarning,
} from "./types";

export interface BuildOptions {
  /**
   * The stored rollup's bounds. When given they seed the chart's extent, so the
   * duration the trace list showed and the width of this chart are the same
   * number rather than two computations that agree until one of them changes.
   * The records still widen it, which is what keeps a truncated trace honest.
   */
  startedAt?: string;
  endedAt?: string;
}

/** Rebuild `records` into a drawable tree. */
export function buildWaterfall(
  records: TraceRecord[],
  opts: BuildOptions = {},
): Waterfall {
  const warnings: WaterfallWarning[] = [];
  const ends = readTimes(records, warnings);
  const bounds = extent(records, ends, opts);
  if (!bounds) {
    return { roots: [], originMs: 0, spanNs: 1, count: 0, warnings };
  }
  const offset = (at: Instant) => nsBetween(bounds.origin, at);

  const nodes: WaterfallNode[] = [];
  const inputs: PreInvoke[] = [];
  for (const record of records) {
    const end = ends.get(record.id);
    if (end === undefined) continue;
    if (record.kind === "block.pre-invoke") {
      inputs.push({ record, at: offset(end) });
    } else if (SPAN_KINDS.has(record.kind)) {
      const start = offset(minusNs(end, record.durationNs));
      nodes.push(spanOf(record, start, offset(end)));
    }
  }

  nodes.push(...inferredRoots(nodes, warnings));
  attachInputs(nodes, inputs);
  const roots = nest(nodes);
  warnings.push(...structuralWarnings(nodes, inputs));

  return {
    roots,
    originMs: bounds.origin.ms,
    spanNs: bounds.spanNs,
    count: nodes.length,
    warnings,
  };
}

/** Every record's end instant, keyed by record id. */
function readTimes(
  records: TraceRecord[],
  warnings: WaterfallWarning[],
): Map<string, Instant> {
  const ends = new Map<string, Instant>();
  let unreadable = 0;
  for (const record of records) {
    const at = parseInstant(record.ts);
    if (at === null) unreadable++;
    else ends.set(record.id, at);
  }
  if (unreadable > 0) {
    warnings.push({
      kind: "unreadable-timestamp",
      message: `${count(unreadable, "record")} carried a timestamp that could not be read, and ${unreadable === 1 ? "is" : "are"} not drawn.`,
    });
  }
  return ends;
}

/** The chart's origin and width, or null when nothing can be placed on it. */
function extent(
  records: TraceRecord[],
  ends: Map<string, Instant>,
  opts: BuildOptions,
): { origin: Instant; spanNs: number } | null {
  let first = opts.startedAt ? parseInstant(opts.startedAt) : null;
  let last = opts.endedAt ? parseInstant(opts.endedAt) : null;

  for (const record of records) {
    const end = ends.get(record.id);
    if (end === undefined) continue;
    const start = minusNs(end, record.durationNs);
    if (first === null || compareInstants(start, first) < 0) first = start;
    if (last === null || compareInstants(end, last) > 0) last = end;
  }
  if (first === null || last === null) return null;
  // At least 1ns wide, so a trace that took no measurable time still divides.
  return { origin: first, spanNs: Math.max(nsBetween(first, last), 1) };
}

function spanOf(record: TraceRecord, start: number, end: number): WaterfallNode {
  return {
    id: record.id,
    kind: record.kind,
    label: labelOf(record),
    path: record.path,
    eventId: record.eventId,
    start,
    end,
    durationNs: record.durationNs,
    seq: record.seq,
    record,
    input: null,
    inputMatched: true,
    inferred: false,
    lane: 0,
    depth: 0,
    children: [],
  };
}

/** How a row is named: the block's own label, the route a request came in on,
 * or the flow — whichever this record actually knows. */
function labelOf(record: TraceRecord): string {
  if (record.path !== "") return blockLabel(record.path);
  if (record.kind === "source.respond") {
    const method = String(record.attrs.method ?? "");
    const route = String(record.attrs.route ?? "");
    const label = [method, route].filter(Boolean).join(" ");
    if (label !== "") return label;
  }
  return record.flow || record.kind;
}

/**
 * A stand-in root for an invocation whose terminal record never arrived.
 *
 * Without one, its blocks would be adopted by whichever unrelated span happened
 * to enclose them in time — a tree that looks complete and is wrong. The stand-in
 * spans its members, which is a *lower bound* on what really ran, and says so.
 */
function inferredRoots(
  nodes: WaterfallNode[],
  warnings: WaterfallWarning[],
): WaterfallNode[] {
  const invocations = new Map<string, WaterfallNode[]>();
  for (const node of nodes) {
    const group = invocations.get(node.eventId);
    if (group) group.push(node);
    else invocations.set(node.eventId, [node]);
  }

  const roots: WaterfallNode[] = [];
  for (const [eventId, group] of invocations) {
    // A flow terminal and a source response are the records with no path; one of
    // them is this invocation's root when it survived.
    if (group.some((node) => node.path === "")) continue;
    roots.push({
      id: `inferred:${eventId}`,
      // Not a record kind: nothing published this row, which is the point.
      kind: "invocation",
      // The first member that knows its flow: source records carry none, so
      // reading only group[0] would label a whole invocation "incomplete"
      // whenever one of those happened to sort first.
      label:
        group.find((node) => node.record?.flow)?.record?.flow ??
        "incomplete invocation",
      path: "",
      eventId,
      start: Math.min(...group.map((node) => node.start)),
      end: Math.max(...group.map((node) => node.end)),
      durationNs: 0,
      // Just past its members, so a stand-in wrapping a single span of identical
      // extent still sorts outside it rather than tying with it.
      seq: Math.max(...group.map((node) => node.seq)) + 1,
      record: null,
      input: null,
      inputMatched: true,
      inferred: true,
      lane: 0,
      depth: 0,
      children: [],
    });
  }
  if (roots.length > 0) {
    warnings.push({
      kind: "no-terminal",
      message: `${count(roots.length, "invocation")} never reported an outcome; ${roots.length === 1 ? "its span is" : "their spans are"} inferred from what ran inside ${roots.length === 1 ? "it" : "them"}.`,
    });
  }
  return roots;
}

/** What this trace's own records prove about their own incompleteness. */
function structuralWarnings(
  nodes: WaterfallNode[],
  inputs: PreInvoke[],
): WaterfallWarning[] {
  const warnings: WaterfallWarning[] = [];

  const returned = new Set(
    nodes
      .filter((node) => node.kind === "block.post-invoke")
      .map((node) => addressKey(node.eventId, node.path)),
  );
  const entered = new Set(
    inputs
      .filter((pre) => !returned.has(addressKey(pre.record.eventId, pre.record.path)))
      .map((pre) => pre.record.path),
  );
  if (entered.size > 0) {
    warnings.push({
      kind: "block-never-returned",
      message: `${count(entered.size, "block")} was entered and never reported an outcome: ${[...entered].join(", ")}.`,
    });
  }

  const overrun = nodes.filter((node) =>
    node.children.some((child) => child.start < node.start || child.end > node.end),
  );
  if (overrun.length > 0) {
    warnings.push({
      kind: "child-outlives-parent",
      message: `${count(overrun.length, "span")} holds work measured outside its own extent, so the nesting there is drawn from the block addresses rather than from the clock.`,
    });
  }
  return warnings;
}

function count(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}
