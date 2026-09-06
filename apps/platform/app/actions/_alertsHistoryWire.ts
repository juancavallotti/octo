/**
 * The wire shapes for what alerting has done: episodes, and the execution log.
 *
 * Split from `_alertsWire.ts`, which carries the definition side. They are two
 * halves of one API and one file was over the line cap, but the seam is a real
 * one: a watch is what somebody wrote, and these are what it did.
 */

import type {
  AlertOutcome,
  Evaluation,
  Incident,
  WatchPreview,
} from "@/app/model/alerts";

/**
 * An outcome as the service emits it. Already camelCase on the wire, because it
 * is serialized from the domain type rather than shaped by the handler — so this
 * is a pass-through rather than a mapping.
 */
export type RawOutcome = AlertOutcome;

export interface RawIncident {
  id: string;
  watch_id: string;
  watch_name: string;
  opened_at: string;
  resolved_at: string | null;
  closed_reason?: string;
  severity: string;
  acknowledged_at: string | null;
  opened_matched: number;
  opened_total: number;
  opened_outcomes: RawOutcome[];
  evaluations: number;
  notifications: number;
}

export interface RawIncidentList {
  items: RawIncident[];
}

export interface RawEvaluation {
  id: string;
  watch_id: string;
  incident_id?: string;
  evaluated_at: string;
  status: string;
  phase: string;
  previous_phase: string;
  transitioned: boolean;
  degraded: boolean;
  matched: number;
  total: number;
  window_from: string | null;
  window_to: string | null;
  reason?: string;
  error?: string;
  duration_ms: number;
  outcomes: RawOutcome[];
}

export interface RawEvaluationList {
  items: RawEvaluation[];
  next_before?: string;
}

export interface RawPreview {
  status: string;
  verdict: string;
  matched: number;
  total: number;
  degraded: boolean;
  window_from: string;
  window_to: string;
  outcomes: RawOutcome[];
}

export interface RawPreview {
  status: string;
  verdict: string;
  matched: number;
  total: number;
  degraded: boolean;
  window_from: string;
  window_to: string;
  outcomes: RawOutcome[];
}

export function toIncident(r: RawIncident): Incident {
  return {
    id: r.id,
    watchId: r.watch_id,
    watchName: r.watch_name,
    openedAt: r.opened_at,
    resolvedAt: r.resolved_at,
    closedReason: r.closed_reason,
    severity: r.severity as Incident["severity"],
    acknowledgedAt: r.acknowledged_at,
    openedMatched: r.opened_matched,
    openedTotal: r.opened_total,
    openedOutcomes: r.opened_outcomes ?? [],
    evaluations: r.evaluations,
    notifications: r.notifications,
  };
}

export function toEvaluation(r: RawEvaluation): Evaluation {
  return {
    id: r.id,
    watchId: r.watch_id,
    incidentId: r.incident_id,
    evaluatedAt: r.evaluated_at,
    status: r.status as Evaluation["status"],
    phase: r.phase as Evaluation["phase"],
    previousPhase: r.previous_phase as Evaluation["previousPhase"],
    transitioned: r.transitioned,
    degraded: r.degraded,
    matched: r.matched,
    total: r.total,
    windowFrom: r.window_from,
    windowTo: r.window_to,
    reason: r.reason,
    error: r.error,
    durationMs: r.duration_ms,
    outcomes: r.outcomes ?? [],
  };
}

export function toPreview(r: RawPreview): WatchPreview {
  return {
    status: r.status as WatchPreview["status"],
    verdict: r.verdict as WatchPreview["verdict"],
    matched: r.matched,
    total: r.total,
    degraded: r.degraded,
    windowFrom: r.window_from,
    windowTo: r.window_to,
    outcomes: r.outcomes ?? [],
  };
}
