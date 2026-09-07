"use server";

/**
 * Server actions for alerting. Authorizes, then delegates to the alerting client
 * (`_alerts.ts`), which reads and writes the observability service's API
 * directly.
 *
 * Reads take any signed-in caller; every mutation takes the write roles. These
 * are the first writes this app makes to the observability service besides the
 * retention policy — that service has no auth of its own, and this is the
 * boundary.
 *
 * The acting user is resolved here, from the session, through the same gate that
 * resolves it everywhere else — the durable orchestrator user id rather than
 * whatever the session object happens to carry, which is what makes it work with
 * SSO off. It is never taken from client input. Attribution rather than
 * authorization, but taking it from the request would let one user's change be
 * recorded against another.
 */

import type {
  Evaluation,
  EvaluationPage,
  EvaluationQuery,
  Incident,
  IncidentQuery,
  Watch,
  WatchInput,
  WatchListItem,
  WatchPreview,
} from "@/app/model/alerts";
import { withRead, withWrite, withWriteUser } from "./_auth";
import * as alerts from "./_alerts";
import type { ActionResult } from "@octo/http";

export async function listWatches(): Promise<ActionResult<WatchListItem[]>> {
  return withRead(() => alerts.listWatches());
}

export async function getWatch(id: string): Promise<ActionResult<Watch>> {
  return withRead(() => alerts.getWatch(id));
}

export async function createWatch(
  input: WatchInput,
): Promise<ActionResult<Watch>> {
  return withWriteUser((userId) => alerts.createWatch(input, userId));
}

export async function saveWatch(
  id: string,
  input: WatchInput,
): Promise<ActionResult<Watch>> {
  return withWriteUser((userId) => alerts.saveWatch(id, input, userId));
}

export async function deleteWatch(id: string): Promise<ActionResult<void>> {
  return withWrite(() => alerts.deleteWatch(id));
}

/**
 * Evaluate a definition without storing it. A write, despite reading nothing
 * back: it runs real queries against the telemetry stores on demand, which is
 * not something a read-only caller should be able to trigger at will.
 */
export async function previewWatch(
  input: WatchInput,
): Promise<ActionResult<WatchPreview>> {
  return withWrite(() => alerts.previewWatch(input));
}

export async function muteWatch(
  id: string,
  until: string | null,
): Promise<ActionResult<void>> {
  return withWrite(() => alerts.muteWatch(id, until));
}

export async function listIncidents(
  query: IncidentQuery,
): Promise<ActionResult<Incident[]>> {
  return withRead(() => alerts.listIncidents(query));
}

export async function acknowledgeIncident(
  id: string,
): Promise<ActionResult<void>> {
  return withWriteUser((userId) => alerts.acknowledgeIncident(id, userId));
}

export async function listEvaluations(
  query: EvaluationQuery,
): Promise<ActionResult<EvaluationPage>> {
  return withRead(() => alerts.listEvaluations(query));
}

// Re-exported so a caller can name a page's rows without importing the model
// twice; the model is where these are declared.
export type { Evaluation };
