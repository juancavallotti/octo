"use server";

/**
 * Server action for the platform-services report.
 *
 * A read, so it takes only a session rather than the write roles: knowing whether
 * Redis is up configures nothing and reveals nothing a signed-in user could act
 * on destructively. The detail strings are transport errors from the
 * orchestrator's own probes and carry no credentials.
 */

import { withRead } from "./_auth";
import * as client from "./client/health";
import type { ActionResult } from "./_client";
import type { HealthReport } from "./client/health";

export async function getHealth(): Promise<ActionResult<HealthReport>> {
  return withRead(() => client.getHealth());
}
