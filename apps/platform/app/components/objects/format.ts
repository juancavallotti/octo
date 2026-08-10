import type { DeploymentWithIntegration } from "@/app/model/orchestrator";

/**
 * A readable label for a deployment in the object browser's picker.
 *
 * It is also the sort key, which is the reason it is worth having on its own: the
 * list is ordered by what the reader sees rather than by id or by insertion, so
 * the label and the ordering cannot drift apart.
 */
export function deploymentLabel(d: DeploymentWithIntegration): string {
  return `${d.integrationName}${d.tag ? ` · ${d.tag}` : ""} (${d.status})`;
}
