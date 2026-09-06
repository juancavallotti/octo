"use server";

/**
 * Server action for the platform logs view. Authorizes (any signed-in caller) and
 * delegates to the stored-logs client (`_logs.ts`), which reads the
 * observability service's API directly. The model unwraps the ActionResult. Read-only —
 * stored logs are never mutated here.
 */

import type { LogFilters, LogPage } from "@/app/model/logs";
import { withRead } from "./_auth";
import * as logs from "./_logs";
import type { ActionResult } from "./_client";

export async function listLogs(
  filters: LogFilters,
): Promise<ActionResult<LogPage>> {
  return withRead(() => logs.getLogs(filters));
}
