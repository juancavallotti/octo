import { describe, expect, it } from "vitest";
import { toDeploymentInput } from "./deployInput";

describe("toDeploymentInput", () => {
  it("forwards every setting the form chose", () => {
    expect(
      toDeploymentInput(
        {
          replicas: 2,
          tracing: true,
          runner: "agentic",
          runtimeVersion: "0.11.13",
        },
        "snap-1",
      ),
    ).toEqual({
      snapshotId: "snap-1",
      replicas: 2,
      tracing: true,
      runner: "agentic",
      runtimeVersion: "0.11.13",
    });
  });

  it("leaves out what the form did not set", () => {
    expect(toDeploymentInput({ replicas: 1 }, "snap-1")).toEqual({
      snapshotId: "snap-1",
      replicas: 1,
    });
  });
});
