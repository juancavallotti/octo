/**
 * Browser-side client for the site's data-retention policy. Backed by the server
 * actions in `app/actions/retention.ts`, which read and write the aggregator's
 * retention API directly; these wrappers unwrap the ActionResult so callers keep
 * a value-or-throw contract.
 *
 * It sits beside `logs.ts` and `traces.ts` rather than inside `siteSettings.ts`
 * because it reaches the same service they do. The email and LLM settings live
 * on the orchestrator; retention lives on the aggregator, which owns the tables
 * a policy governs. The types live here for the same reason theirs do — the
 * client that shapes the wire is server-only, and nothing client-side should
 * have to import it to name what comes back.
 */

import * as actions from "@/app/actions/retention";
import { unwrap } from "./bff";

/** The site's retention policy. */
export interface RetentionPolicy {
  /** Days of log events kept. Zero means forever. */
  logsDays: number;
  /** Days of traces kept, on its own axis. Zero means forever. */
  tracesDays: number;
  /**
   * Days of alerting history kept — evaluations, and the episodes that have
   * closed. Zero means forever here too, but unlike the other two it is not the
   * default: an installation that has never configured retention reads back 14,
   * because a watch writes an evaluation row every time it runs whether or not
   * anything happened.
   */
  alertsDays: number;
  /** RFC3339 timestamp of the last save; null if never configured. */
  updatedAt: string | null;
}

/**
 * One save. Every window goes on every request: the aggregator refuses a partial
 * policy, because an omitted field would be indistinguishable from a request to
 * keep that stream forever.
 */
export interface RetentionPolicyInput {
  logsDays: number;
  tracesDays: number;
  alertsDays: number;
}

/** What one sweep deleted. */
export interface RetentionRun {
  logsDeleted: number;
  /** Individual trace records, of which a trace has many. */
  tracesDeleted: number;
  /** Whole traces — the unit retention actually works in. */
  traceSummariesDeleted: number;
  /**
   * The instant this sweep deleted below, RFC3339; null when the axis is set to
   * keep everything. That distinction is the only thing separating a zero count
   * nobody asked for from one where nothing had expired.
   */
  logsCutoff: string | null;
  tracesCutoff: string | null;
  /** Evaluation rows removed, and separately the closed episodes. */
  alertEvaluationsDeleted: number;
  alertIncidentsDeleted: number;
  alertsCutoff: string | null;
  durationMs: number;
}

/** Read the stored policy. Zero on an axis means keep forever. */
export async function getRetention(): Promise<RetentionPolicy> {
  return unwrap(await actions.getRetention());
}

/** Save the policy. Every window is sent on every save. */
export async function saveRetention(
  input: RetentionPolicyInput,
): Promise<RetentionPolicy> {
  return unwrap(await actions.saveRetention(input));
}

/** Enforce the stored policy now. What it deletes does not come back. */
export async function runRetention(): Promise<RetentionRun> {
  return unwrap(await actions.runRetention());
}
