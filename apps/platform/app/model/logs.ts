/**
 * Browser-side client for the platform logs view. Backed by the `listLogs` server
 * action, which reads the observability service's API directly; this wrapper
 * unwraps the ActionResult so callers keep a value-or-throw contract. Read-only:
 * the /platform/logs view fetches pages of stored log events with filters.
 */

import * as logActions from "@/app/actions/logs";
import { unwrap } from "./bff";

/** One stored log event, attributed to the deployment that emitted it. */
export interface LogEntry {
  id: string;
  /** Deployment uuid the event came from. */
  deploymentId: string;
  /** Deployment display name as it was on the emitting pod (may be empty). */
  appName: string;
  /** Deployment tag/version as it was on the emitting pod (may be empty). */
  appVersion: string;
  /** RFC3339 timestamp the record carried. */
  ts: string;
  /** slog level, e.g. "INFO" / "ERROR". */
  level: string;
  message: string;
  /** Remaining structured slog fields. */
  attrs: Record<string, unknown>;
  /** RFC3339 timestamp the aggregator stored the row. */
  receivedAt: string;
}

/** One page of log events, newest first, with a keyset cursor for the next page. */
export interface LogPage {
  items: LogEntry[];
  /**
   * Pass as `before` to fetch the next (older) page; null on the last page.
   * Treat it as opaque — the format is the service's to change.
   */
  nextBefore: string | null;
}

/** Filters narrowing a log query; every field is optional ("no constraint"). */
export interface LogFilters {
  /** Limit to one deployment. */
  deploymentId?: string;
  /** Limit to one app by its display name (exact match). */
  appName?: string;
  /** Limit to one app version/tag (exact match). */
  appVersion?: string;
  /** Limit to these levels (e.g. ["ERROR", "WARN"]). */
  levels?: string[];
  /** RFC3339 lower/upper bounds on the record timestamp. */
  from?: string;
  to?: string;
  /** Case-insensitive substring match on the message. */
  q?: string;
  /**
   * Keyset cursor from a previous page's `nextBefore`. Opaque: it names a
   * position, not a time. A timestamp alone cannot name one, because several
   * rows can carry the same timestamp — the log lines from a single request
   * are all stamped from one clock read. Pass it back unchanged.
   */
  before?: string;
  /** Page size (the service clamps it to a sane maximum). */
  limit?: number;
}

/** Fetch one page of stored log events matching the filters. */
export async function listLogs(filters: LogFilters = {}): Promise<LogPage> {
  return unwrap(await logActions.listLogs(filters));
}
