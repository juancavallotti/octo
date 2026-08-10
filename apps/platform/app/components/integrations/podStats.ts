import type { Deployment } from "@/app/model/orchestrator";

/**
 * The two pure readings the deployments list takes off a deployment. Neither
 * touches React, so both can be checked directly — and both are the kind of
 * one-liner that is quietly wrong for years otherwise.
 */

/** Total container restarts across a deployment's pods. */
export function totalRestarts(d: Deployment): number {
  return (d.pods ?? []).reduce((sum, p) => sum + p.restarts, 0);
}

/** Strip the scheme so an address reads as a bare host[:port]/path. */
export function bareHost(url: string): string {
  return url.replace(/^https?:\/\//, "");
}
