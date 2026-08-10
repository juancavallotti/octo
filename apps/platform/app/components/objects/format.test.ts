import { describe, expect, it } from "vitest";

import { deploymentLabel } from "./format";

import type { DeploymentWithIntegration } from "@/app/model/orchestrator";

const deployment = (over: Partial<DeploymentWithIntegration>) =>
  ({
    id: "d1",
    integrationName: "Orders",
    status: "running",
    ...over,
  }) as DeploymentWithIntegration;

describe("deploymentLabel", () => {
  it("names the integration, its version and its status", () => {
    expect(deploymentLabel(deployment({ tag: "v2.0" }))).toBe(
      "Orders · v2.0 (running)",
    );
  });

  // An untagged deployment is the working copy. The separator has to go with the
  // tag, or every such row reads as "Orders ·  (running)".
  it("drops the separator with the tag", () => {
    expect(deploymentLabel(deployment({ tag: "" }))).toBe("Orders (running)");
    expect(deploymentLabel(deployment({}))).toBe("Orders (running)");
  });
});
