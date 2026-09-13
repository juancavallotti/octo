/**
 * Whether the orchestrator can reach what this installation runs on. Shallow by
 * definition: a reachable dependency is one that answered a single round trip,
 * not one that is healthy in any deeper sense.
 */

import type { ActionResult } from "@octo/http";
import { call } from "./http";

/** One dependency's answer. */
export interface Dependency {
  /** postgres | redis | nats | kubernetes */
  name: string;
  /**
   * Whether this installation has the dependency at all. False is not a failure —
   * an orchestrator with no cluster access is a supported way to run.
   */
  configured: boolean;
  reachable: boolean;
  /** Why it did not answer, when it did not. */
  detail?: string;
  /** How long the round trip took, when one was made. */
  latencyMs?: number;
}

export interface HealthReport {
  dependencies: Dependency[];
}

export function getHealth(): Promise<ActionResult<HealthReport>> {
  return call<HealthReport>("GET", "/settings/health");
}
