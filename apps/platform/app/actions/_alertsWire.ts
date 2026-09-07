/**
 * The wire shapes for alerting, mirroring `observability/internal/api/alerts.go`.
 *
 * Separate from the client for the same reason `_statsWire.ts` is: the mapping is
 * most of the code, it is the part that has to be checked field for field against
 * the service, and reading it beside the requests would bury both.
 *
 * The episode and execution-log shapes live in `_alertsHistoryWire.ts`; this file
 * is the definition side.
 *
 * Conditions and actions cross as open objects, exactly as the service stores
 * them. Typing them here would put a second definition of every kind's parameters
 * next to the one the service already decodes strictly — and the service's own
 * error message, which names the field, is what the editor shows.
 */

import type {
  AlertAction,
  AlertCondition,
  Watch,
  WatchInput,
  WatchListItem,
  WatchState,
} from "@/app/model/alerts";

/** A watch as the service emits it (snake_case). */
export interface RawWatch {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  severity: string;
  combinator: string;
  conditions: Record<string, unknown>[];
  actions: Record<string, unknown>[];
  on_no_data: string;
  step_seconds: number;
  interval_seconds: number;
  for_seconds: number;
  cooldown_seconds: number;
  created_at?: string | null;
  updated_at?: string | null;
}

export interface RawState {
  phase: string;
  since: string | null;
  consecutive_firing: number;
  consecutive_ok: number;
  last_eval_at: string | null;
  last_status: string;
  last_value: number | null;
  incident_id?: string;
  muted_until: string | null;
  next_due_at: string | null;
}

export interface RawWatchList {
  items: { watch: RawWatch; state: RawState }[];
}

export function toWatch(r: RawWatch): Watch {
  return {
    id: r.id,
    name: r.name,
    description: r.description,
    enabled: r.enabled,
    severity: r.severity as Watch["severity"],
    combinator: r.combinator as Watch["combinator"],
    conditions: (r.conditions ?? []) as unknown as AlertCondition[],
    actions: (r.actions ?? []) as unknown as AlertAction[],
    onNoData: r.on_no_data as Watch["onNoData"],
    stepSeconds: r.step_seconds,
    intervalSeconds: r.interval_seconds,
    forSeconds: r.for_seconds,
    cooldownSeconds: r.cooldown_seconds ?? 0,
    createdAt: r.created_at ?? null,
    updatedAt: r.updated_at ?? null,
  };
}

/** The body of a save. The id travels in the path, never in the body. */
export function fromWatch(w: WatchInput): Record<string, unknown> {
  return {
    name: w.name,
    description: w.description,
    enabled: w.enabled,
    severity: w.severity,
    combinator: w.combinator,
    conditions: w.conditions,
    actions: w.actions,
    on_no_data: w.onNoData,
    step_seconds: w.stepSeconds,
    interval_seconds: w.intervalSeconds,
    for_seconds: w.forSeconds,
    cooldown_seconds: w.cooldownSeconds,
  };
}

export function toState(r: RawState): WatchState {
  return {
    phase: r.phase as WatchState["phase"],
    since: r.since,
    consecutiveFiring: r.consecutive_firing,
    consecutiveOk: r.consecutive_ok,
    lastEvalAt: r.last_eval_at,
    lastStatus: r.last_status,
    lastValue: r.last_value,
    incidentId: r.incident_id,
    mutedUntil: r.muted_until,
    nextDueAt: r.next_due_at,
  };
}

export function toListItem(r: {
  watch: RawWatch;
  state: RawState;
}): WatchListItem {
  return { watch: toWatch(r.watch), state: toState(r.state) };
}
