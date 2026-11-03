import type { DeploymentInput } from "@/app/model/orchestrator";
import type { DeploySubmit } from "./DeployModal";

/** The deployment request for a submitted form, once its version is a snapshot. */
export function toDeploymentInput(
  input: DeploySubmit,
  snapshotId: string,
): DeploymentInput {
  return {
    snapshotId,
    replicas: input.replicas,
    ...(input.slug ? { slug: input.slug } : {}),
    ...(input.expose ? { expose: input.expose } : {}),
    ...(input.env ? { env: input.env } : {}),
    ...(input.tracing ? { tracing: true } : {}),
    ...(input.access?.length ? { access: input.access } : {}),
    ...(input.runner ? { runner: input.runner } : {}),
    ...(input.runtimeVersion ? { runtimeVersion: input.runtimeVersion } : {}),
  };
}
