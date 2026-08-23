/**
 * Browser-side client for the platform-services report. Backed by the server
 * action in `app/actions/health.ts`; this wrapper unwraps the ActionResult so
 * callers keep a value-or-throw contract.
 */

import * as actions from "@/app/actions/health";
import { unwrap } from "./bff";

export type { Dependency, HealthReport } from "@/app/actions/client/health";

import type { HealthReport } from "@/app/actions/client/health";

/** Ask the orchestrator what it can currently reach. */
export async function getHealth(): Promise<HealthReport> {
  return unwrap(await actions.getHealth());
}
