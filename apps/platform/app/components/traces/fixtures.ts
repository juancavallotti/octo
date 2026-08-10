/**
 * Building trace records for tests, in the shape the runtime actually publishes
 * them: stamped at completion, carrying how long the thing took, so a span is
 * `[ts - durationNs, ts]`.
 *
 * The helpers take a **start** offset and a duration, because that is how a
 * scenario is described ("the fork's first branch ran from 10ms for 30ms") — and
 * then write the record the way the runtime would, from the far end. Getting that
 * inversion wrong in the fixtures would make every test agree with a builder that
 * read the records backwards.
 */

import type { TraceRecord } from "@/app/model/traces";

/** The instant offset 0 sits at, chosen to be a round wall-clock time. */
export const T0 = "2026-08-09T10:00:00.000000000Z";

const T0_MS = Date.parse(T0);

let nextId = 0;

/** Reset the id counter so ids are stable within one test. */
export function resetIds(): void {
  nextId = 0;
}

/** An RFC3339 timestamp `ns` nanoseconds after {@link T0}, to full precision. */
export function at(ns: number): string {
  const ms = Math.floor(ns / 1_000_000);
  const rest = ns - ms * 1_000_000;
  const iso = new Date(T0_MS + ms).toISOString(); // ends in ".mmmZ"
  return `${iso.slice(0, -1)}${String(rest).padStart(6, "0")}Z`;
}

export interface RecordSpec {
  kind: string;
  /** Nanoseconds after T0 at which the traced thing began. */
  start?: number;
  /** How long it took; 0 marks an instant. */
  duration?: number;
  path?: string;
  eventId?: string;
  flow?: string;
  blockType?: string;
  seq?: number;
  id?: string;
  attrs?: Record<string, unknown>;
  body?: unknown;
}

/**
 * One record, with everything a test did not care about filled in.
 *
 * `seq` defaults to the record's position, which is publication order — so a
 * composite listed after the blocks inside it gets the higher sequence, exactly
 * as the runtime would publish it (a composite finishes last).
 */
export function record(spec: RecordSpec): TraceRecord {
  const start = spec.start ?? 0;
  const duration = spec.duration ?? 0;
  const n = nextId++;
  return {
    id: spec.id ?? `r${n}`,
    traceId: "tr-1",
    seq: spec.seq ?? n,
    kind: spec.kind,
    eventId: spec.eventId ?? "ev-root",
    correlationId: "",
    flow: spec.flow ?? "",
    path: spec.path ?? "",
    blockType: spec.blockType ?? "",
    ts: at(start + duration),
    durationNs: duration,
    error: "",
    dropped: false,
    truncated: false,
    body: spec.body ?? null,
    vars: null,
    attrs: spec.attrs ?? {},
    deploymentId: "dep-1",
    appName: "Orders",
    appVersion: "v1.0",
    model: "",
    provider: "",
    inputTokens: null,
    outputTokens: null,
    thinkingTokens: null,
    cachedTokens: null,
    cacheWriteTokens: null,
    costUsd: null,
    costStatus: "",
    priceId: "",
  };
}

/** Shorthand for the records a whole trace is made of. */
export function records(...specs: RecordSpec[]): TraceRecord[] {
  resetIds();
  return specs.map(record);
}
