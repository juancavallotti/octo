import type { TraceRecord } from "@/app/model/traces";
import { blockLabel } from "./blockPath";

/**
 * What a row stands for, when it stands for more than one record.
 *
 * The aggregator folds a run of near-identical records into a single row — a
 * streaming block emits one per frame, which for an agent answering is one per
 * token — and writes what it collapsed under `attrs.folded`. Reading it here is
 * what stops a folded span looking like a single frame that happened to take ten
 * seconds.
 */

/** The number of records behind a row, or 0 when the row is just itself. */
export function foldedCount(record: TraceRecord): number {
  const folded = record.attrs.folded;
  if (typeof folded !== "object" || folded === null) return 0;
  const count = (folded as { count?: unknown }).count;
  return typeof count === "number" && count > 1 ? count : 0;
}

/**
 * Whether a folded row's body is only the first record's rather than the run's
 * merged text.
 *
 * The aggregator merges the payload when it recognises the field it lives in, and
 * says so when it could not. A reader looking at one frame of a thousand needs to
 * know that is what they are looking at.
 */
export function bodyIsFirstOnly(record: TraceRecord): boolean {
  const folded = record.attrs.folded;
  if (typeof folded !== "object" || folded === null) return false;
  return (folded as { bodies?: unknown }).bodies === "first";
}

/**
 * How a row is named: the block's own label, the route a request came in on, or
 * the flow — whichever this record actually knows — with the fold's count on the
 * end when the row stands for more than itself.
 *
 * It lives here rather than beside the rest of the waterfall because the count is
 * part of the name. A folded row whose label said only "sse-event" would claim to
 * be one frame while spanning a thousand, and the two facts are decided together.
 */
export function spanLabel(record: TraceRecord): string {
  if (record.path !== "") {
    const count = foldedCount(record);
    const label = blockLabel(record.path);
    return count > 0 ? `${label} \u00d7${count}` : label;
  }
  if (record.kind === "source.respond") {
    const method = String(record.attrs.method ?? "");
    const route = String(record.attrs.route ?? "");
    const label = [method, route].filter(Boolean).join(" ");
    if (label !== "") return label;
  }
  return record.flow || record.kind;
}
