/**
 * Browser-side client for alerting. Backed by the server actions in
 * `app/actions/alerts.ts`, which read and write the observability service's API
 * directly; these wrappers unwrap the ActionResult so callers keep a
 * value-or-throw contract.
 *
 * It sits beside `logs.ts`, `traces.ts` and `retention.ts` for the same reason
 * they do: alerting lives on the observability service, which owns both the
 * tables a watch is stored in and the ones it reads. The types live here rather
 * than in the client that shapes the wire, so nothing client-side has to import a
 * server-only module to name what comes back.
 */

import * as actions from "@/app/actions/alerts";
import { unwrap } from "./bff";

/** Which store a condition reads. */
export type AlertSource = "traces" | "logs" | "pod_stats";

/** How the rows inside one bucket collapse to a number. */
export type AlertAggregate =
  "count" | "sum" | "avg" | "min" | "max" | "p95" | "ratio";

/** The three condition kinds. */
export type AlertConditionKind = "threshold" | "spike" | "absence";

export type AlertOp = "gt" | "gte" | "lt" | "lte";

/** What a watch does when it fires. */
export type AlertActionKind = "topic" | "email";

/**
 * How an absent measurement is read. `ok` is the default, because the ordinary
 * reason a window is empty is that an app was quiet; a watch that should fire on
 * silence says so with an `absence` condition, where it is visible.
 */
export type AlertNoData = "ok" | "fire" | "keep";

export type AlertPhase = "ok" | "pending" | "firing" | "invalid";

export type AlertStatus =
  "ok" | "firing" | "insufficient" | "skipped" | "error";

export type AlertSeverity = "info" | "warning" | "critical";

/** What a condition is narrowed to. An absent field is no constraint. */
export interface AlertScope {
  deploymentId?: string;
  integrationId?: string;
  appName?: string;
  appVersion?: string;
  levels?: string[];
  search?: string;
  pods?: string[];
  across?: AlertAggregate;
}

/**
 * One condition, in the shape the service stores and evaluates.
 *
 * `params` is deliberately open: the three kinds share almost no parameters, and
 * the service's own decoder is what refuses a malformed one — with a message
 * naming the field, which the editor surfaces rather than replacing.
 */
export interface AlertCondition {
  id: string;
  type: AlertConditionKind;
  source: AlertSource;
  metric: string;
  aggregate?: AlertAggregate;
  scope?: AlertScope;
  params?: Record<string, unknown>;
}

export interface AlertAction {
  id: string;
  type: AlertActionKind;
  params?: Record<string, unknown>;
}

/** A standing question: one schedule over a set of conditions. */
export interface Watch {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  severity: AlertSeverity;
  /** How the conditions are joined. */
  combinator: "all" | "any";
  conditions: AlertCondition[];
  actions: AlertAction[];
  onNoData: AlertNoData;
  /** The bucket width every condition is measured in. */
  stepSeconds: number;
  /** How often the whole set is asked. */
  intervalSeconds: number;
  /** How long the combined verdict must hold. Counted in evaluations. */
  forSeconds: number;
  /** How often a still-firing watch says so again. Zero announces once. */
  renotifySeconds: number;
  createdAt?: string | null;
  updatedAt?: string | null;
}

/** Where a watch's state machine has got to. */
export interface WatchState {
  phase: AlertPhase;
  since: string | null;
  consecutiveFiring: number;
  consecutiveOk: number;
  lastEvalAt: string | null;
  lastStatus: string;
  lastValue: number | null;
  incidentId?: string;
  mutedUntil: string | null;
  nextDueAt: string | null;
}

export interface WatchListItem {
  watch: Watch;
  state: WatchState;
}

/**
 * One condition's answer, carrying the threshold it was judged against as it was
 * at the time. A row from three weeks ago still explains itself after the watch
 * has been retuned, which it could not if the page looked the threshold up.
 */
export interface AlertOutcome {
  conditionId: string;
  kind: string;
  label: string;
  unit?: string;
  op?: AlertOp;
  threshold: number;
  /** The statistic in units a human recognises; null when there was no data. */
  observed: number | null;
  baseline?: number | null;
  /** The robust z, or the confidence bound the comparison was actually made against. */
  score?: number | null;
  samples: number;
  baselineSamples?: number;
  denominator?: number;
  windowFrom: string;
  windowTo: string;
  verdict: "true" | "false" | "unknown";
  noData?: boolean;
  /** Which gate declined, for the "why did this not fire" question. */
  reason?: string;
  error?: string;
}

/** One firing episode. */
export interface Incident {
  id: string;
  watchId: string;
  watchName: string;
  openedAt: string;
  resolvedAt: string | null;
  /** `resolved` and `stale` are deliberately different facts. */
  closedReason?: string;
  severity: AlertSeverity;
  acknowledgedAt: string | null;
  openedMatched: number;
  openedTotal: number;
  openedOutcomes: AlertOutcome[];
  evaluations: number;
  notifications: number;
}

/** One row of the execution log. */
export interface Evaluation {
  id: string;
  watchId: string;
  incidentId?: string;
  evaluatedAt: string;
  status: AlertStatus;
  phase: AlertPhase;
  previousPhase: AlertPhase;
  transitioned: boolean;
  /** At least one condition could not be answered. */
  degraded: boolean;
  matched: number;
  total: number;
  windowFrom: string | null;
  windowTo: string | null;
  reason?: string;
  error?: string;
  durationMs: number;
  outcomes: AlertOutcome[];
}

/** One page of history. `nextBefore` is opaque; pass it back as `before`. */
export interface EvaluationPage {
  items: Evaluation[];
  nextBefore: string | null;
}

/** What a definition would have decided right now. */
export interface WatchPreview {
  status: AlertStatus;
  verdict: "true" | "false" | "unknown";
  matched: number;
  total: number;
  degraded: boolean;
  windowFrom: string;
  windowTo: string;
  outcomes: AlertOutcome[];
}

/** A watch as it is written. The id is assigned by the service on create. */
export type WatchInput = Omit<Watch, "id" | "createdAt" | "updatedAt"> & {
  id?: string;
};

export interface EvaluationQuery {
  watchId?: string;
  incidentId?: string;
  /** Drop the rows that say nothing happened. */
  notable?: boolean;
  statuses?: AlertStatus[];
  from?: string;
  to?: string;
  before?: string;
  limit?: number;
}

export interface IncidentQuery {
  watchId?: string;
  open?: boolean;
  from?: string;
  to?: string;
  limit?: number;
}

/** Every watch with its current state, newest first. */
export async function listWatches(): Promise<WatchListItem[]> {
  return unwrap(await actions.listWatches());
}

export async function getWatch(id: string): Promise<Watch> {
  return unwrap(await actions.getWatch(id));
}

export async function createWatch(input: WatchInput): Promise<Watch> {
  return unwrap(await actions.createWatch(input));
}

export async function saveWatch(id: string, input: WatchInput): Promise<Watch> {
  return unwrap(await actions.saveWatch(id, input));
}

export async function deleteWatch(id: string): Promise<void> {
  return unwrap(await actions.deleteWatch(id));
}

/**
 * Evaluate a definition now without storing anything or notifying anybody. It
 * takes a whole watch rather than an id, because the question is worth asking
 * about a definition that has not been saved.
 */
export async function previewWatch(input: WatchInput): Promise<WatchPreview> {
  return unwrap(await actions.previewWatch(input));
}

/** Suppress notifications until `until`. Null lifts the mute. */
export async function muteWatch(
  id: string,
  until: string | null,
): Promise<void> {
  return unwrap(await actions.muteWatch(id, until));
}

export async function listIncidents(
  query: IncidentQuery = {},
): Promise<Incident[]> {
  return unwrap(await actions.listIncidents(query));
}

export async function acknowledgeIncident(id: string): Promise<void> {
  return unwrap(await actions.acknowledgeIncident(id));
}

export async function listEvaluations(
  query: EvaluationQuery = {},
): Promise<EvaluationPage> {
  return unwrap(await actions.listEvaluations(query));
}
