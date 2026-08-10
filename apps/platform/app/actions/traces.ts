"use server";

/**
 * Server actions for the platform Traces view. Authorizes (any signed-in caller)
 * and delegates to the trace-store client (`_traces.ts`), which reads the trace
 * service's query API directly. The model unwraps the ActionResult. Read-only —
 * stored traces are never mutated here.
 */

import type {
  TraceAppsPage,
  TraceDetail,
  TraceFilters,
  TracePage,
  TraceRecord,
  TraceWindow,
} from "@/app/model/traces";
import { withRead } from "./_auth";
import * as traces from "./_traces";
import type { ActionResult } from "./_client";

export async function listTraceApps(
  window: TraceWindow,
): Promise<ActionResult<TraceAppsPage>> {
  return withRead(() => traces.getApps(window));
}

export async function listTraces(
  filters: TraceFilters,
): Promise<ActionResult<TracePage>> {
  return withRead(() => traces.getTraces(filters));
}

export async function getTrace(
  traceId: string,
  withBodies: boolean,
): Promise<ActionResult<TraceDetail>> {
  return withRead(() => traces.getTrace(traceId, withBodies));
}

export async function getTraceRecord(
  traceId: string,
  id: string,
): Promise<ActionResult<TraceRecord>> {
  return withRead(() => traces.getRecord(traceId, id));
}
