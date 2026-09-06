import { getDeployment, getIntegration } from "@/app/model/orchestrator";

/**
 * What to call a deployment in the metrics heading.
 *
 * The integration's current name rather than the one captured into the deployment
 * at deploy time, so the heading matches the card that was clicked to get here — a
 * renamed integration would otherwise show one name in the list and another on the
 * page it links to. The captured name is the fallback, for a deployment whose
 * integration has since been deleted.
 *
 * Best-effort throughout: stored metrics outlive the deployment they describe, and
 * a page that failed because it could not find a title would be worse than one
 * headed "Metrics", which is what an undefined return leaves it as.
 *
 * Its own module rather than a helper in the page, because a page file's exports
 * are Next's to define and this one is worth testing on its own.
 */
export async function deploymentTitle(
  deploymentId: string,
): Promise<string | undefined> {
  try {
    const deployment = await getDeployment(deploymentId);
    const name = await getIntegration(deployment.integrationId).then(
      (i) => i.name,
      () => deployment.name,
    );
    // An empty name is no more use than none: fall through to "Metrics".
    if (!name) return undefined;
    return deployment.tag ? `${name} · ${deployment.tag}` : name;
  } catch {
    return undefined;
  }
}
