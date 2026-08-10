/**
 * Browser-side client for the platform Traces view. Backed by the trace server
 * actions, which read the trace store's query API directly; this wrapper unwraps
 * the ActionResult so callers keep a value-or-throw contract. Read-only — stored
 * traces are never mutated from here.
 *
 * The shapes below mirror `logs/internal/repo/tracesquery.go`. Two of its rules
 * survive into these types and must survive into every consumer:
 *
 *  - A record's token counts and cost are `number | null`, and null is a fact:
 *    the provider reported no usage, or the model could not be priced. Zero is a
 *    different claim — that the call was free — and nothing here may make it.
 *  - A summary's `costUsd` is a plain number because it is a sum, but it is only
 *    the whole cost when `unpricedCalls` is 0. Otherwise it is a lower bound, and
 *    a reader shown the total without the count has been misled.
 */

import * as traceActions from "@/app/actions/traces";
import { unwrap } from "./bff";

/** How a trace ended. Ranked failed > dropped > ok when records disagree. */
export type TraceStatus = "ok" | "dropped" | "failed";

/**
 * Why a model call's cost is what it is. `""` marks a record that is not a model
 * call at all; the rest say how far pricing got. Only `priced` means the number
 * beside it is complete.
 */
export type CostStatus =
  | ""
  | "priced"
  | "priced_partial"
  | "unpriced_model"
  | "no_usage";

/**
 * One app's trace activity over a window — a row in the list you pick from before
 * looking at any individual trace. An "app" is a (deployment, name, version)
 * triple, so a rollout shows up as two rows rather than one blended one.
 */
export interface TraceApp {
  deploymentId: string;
  /** Empty when the deployment could not be resolved to an integration. */
  integrationId: string;
  appName: string;
  appVersion: string;
  traces: number;
  failed: number;
  lastSeenAt: string;
  /** What could be priced; a lower bound whenever `unpricedCalls` is non-zero. */
  costUsd: number;
  unpricedCalls: number;
  /**
   * Records the publisher admitted losing in this window. Per app and never per
   * trace: the marker carries no trace id, so nothing can say which traces are
   * incomplete — only that some are.
   */
  droppedRecords: number;
}

/** The app list plus the window it was measured over, which the service echoes
 * back because the caller may not have chosen it and every count needs it. */
export interface TraceAppsPage {
  items: TraceApp[];
  from: string;
  to: string;
}

/** One trace's rollup: what the list shows, and what the detail view's summary
 * panel reads, so the two cannot disagree about a number. */
export interface TraceSummary {
  traceId: string;
  deploymentId: string;
  integrationId: string;
  appName: string;
  appVersion: string;
  /** Every deployment that contributed records. More than one means the trace
   * crossed apps — the id rides on the message and survives a queue hop. */
  deploymentIds: string[];
  startedAt: string;
  endedAt: string;
  rootFlow: string;
  entryKind: string;
  entryLabel: string;
  status: TraceStatus;
  rootDurationNs: number;
  /** How many records were rolled up, not how many this client fetched. */
  records: number;
  llmCalls: number;
  inputTokens: number;
  outputTokens: number;
  cachedTokens: number;
  costUsd: number;
  unpricedCalls: number;
  models: string[];
}

/** One stored trace record. A span is `[ts - durationNs, ts]`: the runtime stamps
 * a record at completion, which is why no start/end pairing is needed. */
export interface TraceRecord {
  id: string;
  traceId: string;
  /** Ordering key within the trace. Gapless across the emitting *process*, not
   * within a trace — a gap here is normal and is never data loss. */
  seq: number;
  kind: string;
  /** Re-minted at every sub-invocation boundary, so it groups spans into one
   * invocation of one flow. */
  eventId: string;
  correlationId: string;
  flow: string;
  /** The block's address within its flow; empty on flow and source records. */
  path: string;
  blockType: string;
  ts: string;
  durationNs: number;
  error: string;
  dropped: boolean;
  truncated: boolean;
  /** Captured payloads, verbatim. Null when the runtime captured none *and* when
   * the trace was fetched with `bodies: false` — indistinguishable by design,
   * since a caller that declined them knows it did. */
  body: unknown;
  vars: unknown;
  attrs: Record<string, unknown>;
  deploymentId: string;
  appName: string;
  appVersion: string;
  model: string;
  provider: string;
  inputTokens: number | null;
  outputTokens: number | null;
  /** Already counted inside `outputTokens`. Never add the two. */
  thinkingTokens: number | null;
  cachedTokens: number | null;
  /** Cache creation, billed above the input rate. Null on a record written before
   *  the runtime reported it — unknown, not zero. */
  cacheWriteTokens: number | null;
  costUsd: number | null;
  costStatus: CostStatus;
  priceId: string;
}

/** One trace: the stored rollup, plus every record that fed it. */
export interface TraceDetail {
  summary: TraceSummary;
  /** Ordered by `seq` — publication order, the only total order a publisher gives. */
  items: TraceRecord[];
  /** The trace held more records than one response carries, so a waterfall drawn
   * from these is incomplete in a way the picture itself cannot show. */
  truncated: boolean;
}

/** One page of traces, newest first. */
export interface TracePage {
  items: TraceSummary[];
  /** Pass back as `before` for the next (older) page; null on the last page.
   * Opaque: a `(startedAt, traceId)` pair, because traces routinely tie. */
  nextBefore: string | null;
}

/** RFC3339 bounds on a query. Either may be given alone; both absent reads the
 * service's default window (the last 24 hours). */
export interface TraceWindow {
  from?: string;
  to?: string;
}

/** Filters narrowing a trace list; every field is optional ("no constraint"). */
export interface TraceFilters extends TraceWindow {
  deploymentId?: string;
  integrationId?: string;
  appName?: string;
  appVersion?: string;
  /** Root flow name (exact match). */
  flow?: string;
  statuses?: TraceStatus[];
  /** Keeps traces whose whole span is at least this long. */
  minDurationMs?: number;
  /** Keeps only traces that made at least one model call. */
  hasLlm?: boolean;
  /** Case-insensitive substring over trace id, entry label and root flow. */
  q?: string;
  /** Keyset cursor from a previous page's `nextBefore`. */
  before?: string;
  /** Page size (the service clamps it). */
  limit?: number;
}

/** List the apps that produced traces in a window, most recently active first. */
export async function listTraceApps(
  window: TraceWindow = {},
): Promise<TraceAppsPage> {
  return unwrap(await traceActions.listTraceApps(window));
}

/** Fetch one page of traces matching the filters. */
export async function listTraces(
  filters: TraceFilters = {},
): Promise<TracePage> {
  return unwrap(await traceActions.listTraces(filters));
}

/**
 * Fetch one trace and its records. Pass `bodies: false` to leave the captured
 * payloads — the whole weight of a trace — on the server; a waterfall needs every
 * record's shape and almost none of their contents, and {@link getTraceRecord}
 * fetches the one the reader actually opens.
 */
export async function getTrace(
  traceId: string,
  opts: { bodies?: boolean } = {},
): Promise<TraceDetail> {
  return unwrap(await traceActions.getTrace(traceId, opts.bodies ?? true));
}

/** Fetch one record of one trace, with its captured payloads. */
export async function getTraceRecord(
  traceId: string,
  id: string,
): Promise<TraceRecord> {
  return unwrap(await traceActions.getTraceRecord(traceId, id));
}
