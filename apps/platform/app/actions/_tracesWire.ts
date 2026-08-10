/**
 * The trace service's JSON shapes, and how they become the model's.
 *
 * These interfaces mirror `logs/internal/repo/tracesquery.go` field for field and
 * **must stay in sync with it** — the same contract the Go side keeps with the
 * runtime's wire struct. Kept apart from the client itself so the one file that
 * has to track a Go struct is the one file to review when that struct changes.
 *
 * The mapping is written out by hand rather than done by a generic
 * snake_case→camelCase walk, and that is not tedium: `body`, `vars` and `attrs`
 * are *captured user data*. A generic converter would silently rewrite a traced
 * payload's `{"user_id": 1}` into `{"userId": 1}`, so the trace would no longer
 * show what the flow actually carried. Only the field names we own are renamed.
 */

import type {
  CostStatus,
  TraceApp,
  TraceRecord,
  TraceStatus,
  TraceSummary,
} from "@/app/model/traces";

/** One app's activity as the service emits it. */
export interface RawApp {
  deployment_id: string;
  integration_id: string;
  app_name: string;
  app_version: string;
  traces: number;
  failed: number;
  last_seen_at: string;
  cost_usd: number;
  unpriced_calls: number;
  dropped_records: number;
}

/** One trace's rollup as the service emits it. */
export interface RawSummary {
  trace_id: string;
  deployment_id: string;
  integration_id: string;
  app_name: string;
  app_version: string;
  deployment_ids: string[] | null;
  started_at: string;
  ended_at: string;
  root_flow: string;
  entry_kind: string;
  entry_label: string;
  status: TraceStatus;
  root_duration_ns: number;
  records: number;
  llm_calls: number;
  input_tokens: number;
  output_tokens: number;
  cached_tokens: number;
  cost_usd: number;
  unpriced_calls: number;
  models: string[] | null;
}

/** One stored record as the service emits it. */
export interface RawRecord {
  id: string;
  trace_id: string;
  seq: number;
  kind: string;
  event_id: string;
  correlation_id: string;
  flow: string;
  path: string;
  block_type: string;
  ts: string;
  duration_ns: number;
  error: string;
  dropped: boolean;
  truncated: boolean;
  body: unknown;
  vars: unknown;
  attrs: Record<string, unknown> | null;
  deployment_id: string;
  app_name: string;
  app_version: string;
  model: string;
  provider: string;
  input_tokens: number | null;
  output_tokens: number | null;
  thinking_tokens: number | null;
  cached_tokens: number | null;
  cost_usd: number | null;
  cost_status: CostStatus;
  price_id: string;
}

/** The service's response envelopes. */
export interface RawApps {
  items: RawApp[] | null;
  from: string;
  to: string;
}

export interface RawPage {
  items: RawSummary[] | null;
  next_before?: string;
}

export interface RawDetail {
  summary: RawSummary;
  items: RawRecord[] | null;
  truncated: boolean;
}

export function toApp(r: RawApp): TraceApp {
  return {
    deploymentId: r.deployment_id,
    integrationId: r.integration_id,
    appName: r.app_name,
    appVersion: r.app_version,
    traces: r.traces,
    failed: r.failed,
    lastSeenAt: r.last_seen_at,
    costUsd: r.cost_usd,
    unpricedCalls: r.unpriced_calls,
    droppedRecords: r.dropped_records,
  };
}

export function toSummary(r: RawSummary): TraceSummary {
  return {
    traceId: r.trace_id,
    deploymentId: r.deployment_id,
    integrationId: r.integration_id,
    appName: r.app_name,
    appVersion: r.app_version,
    deploymentIds: r.deployment_ids ?? [],
    startedAt: r.started_at,
    endedAt: r.ended_at,
    rootFlow: r.root_flow,
    entryKind: r.entry_kind,
    entryLabel: r.entry_label,
    status: r.status,
    rootDurationNs: r.root_duration_ns,
    records: r.records,
    llmCalls: r.llm_calls,
    inputTokens: r.input_tokens,
    outputTokens: r.output_tokens,
    cachedTokens: r.cached_tokens,
    costUsd: r.cost_usd,
    unpricedCalls: r.unpriced_calls,
    models: r.models ?? [],
  };
}

export function toRecord(r: RawRecord): TraceRecord {
  return {
    id: r.id,
    traceId: r.trace_id,
    seq: r.seq,
    kind: r.kind,
    eventId: r.event_id,
    correlationId: r.correlation_id,
    flow: r.flow,
    path: r.path,
    blockType: r.block_type,
    ts: r.ts,
    durationNs: r.duration_ns,
    error: r.error,
    dropped: r.dropped,
    truncated: r.truncated,
    // Passed through untouched, nulls included: absence of a payload is a fact
    // about the trace, and `{}` would claim the flow carried an empty document.
    body: r.body ?? null,
    vars: r.vars ?? null,
    // attrs is ours rather than the user's, and an absent metadata bag and an
    // empty one say the same thing — so this one is safe to normalize.
    attrs: r.attrs ?? {},
    deploymentId: r.deployment_id,
    appName: r.app_name,
    appVersion: r.app_version,
    model: r.model,
    provider: r.provider,
    // The five below stay null when they arrive null. `?? 0` here would turn
    // "the provider reported nothing" into "this call was free".
    inputTokens: r.input_tokens,
    outputTokens: r.output_tokens,
    thinkingTokens: r.thinking_tokens,
    cachedTokens: r.cached_tokens,
    costUsd: r.cost_usd,
    costStatus: r.cost_status,
    priceId: r.price_id,
  };
}
