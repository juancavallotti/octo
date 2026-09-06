/**
 * The stored-logs client — the middle layer between the logs server action and
 * the `fetch` abstraction (`@octo/http`):
 *
 *     serverAction (auth) → this client (getLogs()) → requestJson() → fetch
 *
 * It reads the observability service (see `_observability.ts` for why directly
 * and not through the orchestrator). The address and the snake_case→camelCase
 * shaping are internal; callers see only {@link LogPage}.
 */

import { requestJson, type ActionResult } from "@octo/http";
import type { LogEntry, LogFilters, LogPage } from "@/app/model/logs";
import { observabilityBaseUrl, observabilityUnconfigured } from "./_observability";

/** One stored log row as the service emits it (snake_case). */
interface RawLog {
  id: string;
  deployment_id: string;
  app_name: string;
  app_version: string;
  ts: string;
  level: string;
  message: string;
  attrs: Record<string, unknown> | null;
  received_at: string;
}

/** One page as the service emits it. */
interface RawPage {
  items: RawLog[];
  next_before?: string;
}

/** Build the `/logs` query string from the filters, omitting empty axes. */
function queryString(f: LogFilters): string {
  const params = new URLSearchParams();
  if (f.deploymentId) params.set("deploymentId", f.deploymentId);
  if (f.appName) params.set("appName", f.appName);
  if (f.appVersion) params.set("appVersion", f.appVersion);
  for (const level of f.levels ?? []) params.append("level", level);
  if (f.from) params.set("from", f.from);
  if (f.to) params.set("to", f.to);
  if (f.q) params.set("q", f.q);
  if (f.before) params.set("before", f.before);
  if (f.limit != null) params.set("limit", String(f.limit));
  return params.toString();
}

function toEntry(r: RawLog): LogEntry {
  return {
    id: r.id,
    deploymentId: r.deployment_id,
    appName: r.app_name,
    appVersion: r.app_version,
    ts: r.ts,
    level: r.level,
    message: r.message,
    attrs: r.attrs ?? {},
    receivedAt: r.received_at,
  };
}

/**
 * Fetch one page of stored log events matching the filters. Returns an error
 * result when the observability service is unconfigured or unreachable.
 */
export async function getLogs(f: LogFilters): Promise<ActionResult<LogPage>> {
  const base = observabilityBaseUrl();
  if (!base) {
    return observabilityUnconfigured("log query");
  }
  const qs = queryString(f);
  const url = qs ? `${base}/logs?${qs}` : `${base}/logs`;
  const res = await requestJson<RawPage>("GET", url);
  if (!res.ok) return res;
  return {
    ok: true,
    data: {
      items: res.data.items.map(toEntry),
      nextBefore: res.data.next_before ?? null,
    },
  };
}
