/**
 * Whether the orchestrator can reach what this installation runs on.
 *
 * Deliberately shallow, and the page says so: a reachable dependency is one that
 * answered a single round trip, not one that is healthy in any deeper sense. The
 * report is what someone opens when the platform is behaving strangely and the
 * first question is which of the four processes underneath it is actually up.
 */

import type { ActionResult } from "@octo/http";
import { call } from "./http";

/** One dependency's answer. */
export interface Dependency {
  /** postgres | redis | nats | kubernetes */
  name: string;
  /**
   * Whether this installation has the dependency at all. False is not a failure:
   * an orchestrator with no cluster access is a supported way to run, and
   * reporting it as down would send someone looking for a fault that is not there.
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
