import { describe, expect, it } from "vitest";
import type { DeploymentWithIntegration } from "@/app/model/orchestrator";
import { indexSecretUsage } from "./secretUsage";

/** A deployment with just the fields the index reads. */
function deployment(
  over: Partial<DeploymentWithIntegration>,
): DeploymentWithIntegration {
  return {
    id: "d1",
    integrationId: "i1",
    integrationName: "Orders",
    name: "orders",
    status: "running",
    replicas: 1,
    readyReplicas: 1,
    desiredReplicas: 1,
    lastUpdated: "2026-01-01T00:00:00Z",
    ...over,
  } as DeploymentWithIntegration;
}

describe("indexSecretUsage", () => {
  it("indexes secret bindings by secret name", () => {
    const index = indexSecretUsage([
      deployment({ env: { GITHUB_TOKEN: { secret: "GITHUB_TOKEN" } } }),
      deployment({
        id: "d2",
        integrationName: "Labeler",
        env: { TOKEN: { secret: "GITHUB_TOKEN" }, KEY: { secret: "OPENAI_KEY" } },
      }),
    ]);

    expect(index.get("GITHUB_TOKEN")?.map((u) => u.deploymentId)).toEqual([
      "d1",
      "d2",
    ]);
    expect(index.get("OPENAI_KEY")).toHaveLength(1);
  });

  it("collapses a secret bound twice in one deployment into one use", () => {
    const index = indexSecretUsage([
      deployment({
        env: { GITHUB_TOKEN: { secret: "GH" }, GH_TOKEN: { secret: "GH" } },
      }),
    ]);

    expect(index.get("GH")).toHaveLength(1);
    expect(index.get("GH")?.[0].vars).toEqual(["GITHUB_TOKEN", "GH_TOKEN"]);
  });

  it("ignores literal bindings and deployments with no env", () => {
    const index = indexSecretUsage([
      deployment({ env: { LOG_LEVEL: { value: "debug" } } }),
      deployment({ id: "d2" }),
    ]);

    expect(index.size).toBe(0);
  });
});
