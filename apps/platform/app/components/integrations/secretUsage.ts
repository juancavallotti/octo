import type { DeploymentWithIntegration } from "@/app/model/orchestrator";

/**
 * Where one deployment binds a secret: which deployment, and under which
 * environment variable names. A deployment can bind the same secret more than
 * once (a token read as both GITHUB_TOKEN and GH_TOKEN, say), so vars is a list.
 */
export interface SecretUse {
  deploymentId: string;
  integrationId: string;
  integrationName: string;
  /** The deployment's version tag, when it has one. */
  tag?: string;
  /** Env var names in that deployment bound to this secret. */
  vars: string[];
}

/**
 * Index every deployment's env bindings by the secret they reference.
 *
 * The platform already knows this: a deployment's persisted bindings come back on
 * the deployment itself, so which secrets are live is a regrouping of data the
 * pages have rather than a question for the orchestrator. Deployments arrive in
 * whatever order they were listed and each secret's uses keep that order.
 */
export function indexSecretUsage(
  deployments: readonly DeploymentWithIntegration[],
): Map<string, SecretUse[]> {
  const index = new Map<string, SecretUse[]>();
  for (const d of deployments) {
    // One entry per (deployment, secret): the vars accumulate into it, so a
    // deployment binding the same secret twice is one line in the popover.
    const bySecret = new Map<string, string[]>();
    for (const [varName, binding] of Object.entries(d.env ?? {})) {
      if (!binding?.secret) continue;
      const vars = bySecret.get(binding.secret);
      if (vars) vars.push(varName);
      else bySecret.set(binding.secret, [varName]);
    }
    for (const [secret, vars] of bySecret) {
      const uses = index.get(secret) ?? [];
      uses.push({
        deploymentId: d.id,
        integrationId: d.integrationId,
        integrationName: d.integrationName,
        tag: d.tag,
        vars,
      });
      index.set(secret, uses);
    }
  }
  return index;
}
