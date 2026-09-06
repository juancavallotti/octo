/**
 * Naming the deployment a metrics page is showing. Mostly this is about the ways
 * the name can be missing: the page has to keep working when the deployment or the
 * integration behind it has gone, because stored metrics outlive both.
 */

import { beforeEach, describe, expect, it, vi } from "vitest";

const getDeployment = vi.fn();
const getIntegration = vi.fn();
vi.mock("@/app/model/orchestrator", () => ({
  getDeployment: (id: string) => getDeployment(id),
  getIntegration: (id: string) => getIntegration(id),
}));

const { deploymentTitle } = await import("./deploymentTitle");

const DEPLOYMENT = {
  id: "d1",
  integrationId: "i1",
  name: "Captured At Deploy",
  tag: undefined as string | undefined,
};

beforeEach(() => {
  vi.clearAllMocks();
  getDeployment.mockResolvedValue({ ...DEPLOYMENT });
  getIntegration.mockResolvedValue({ id: "i1", name: "Orders" });
});

it("uses the integration's current name", async () => {
  await expect(deploymentTitle("d1")).resolves.toBe("Orders");
  expect(getDeployment).toHaveBeenCalledWith("d1");
  expect(getIntegration).toHaveBeenCalledWith("i1");
});

it("appends the tag when the deployment carries one", async () => {
  getDeployment.mockResolvedValue({ ...DEPLOYMENT, tag: "v2" });
  await expect(deploymentTitle("d1")).resolves.toBe("Orders · v2");
});

describe("when a name is not to be had", () => {
  it("falls back to the name captured at deploy time", async () => {
    getIntegration.mockRejectedValue(new Error("gone"));
    await expect(deploymentTitle("d1")).resolves.toBe("Captured At Deploy");
  });

  it("keeps the tag on that fallback", async () => {
    getDeployment.mockResolvedValue({ ...DEPLOYMENT, tag: "v2" });
    getIntegration.mockRejectedValue(new Error("gone"));
    await expect(deploymentTitle("d1")).resolves.toBe("Captured At Deploy · v2");
  });

  it("gives up on a deployment that is gone, rather than throwing", async () => {
    getDeployment.mockRejectedValue(new Error("404"));
    await expect(deploymentTitle("d1")).resolves.toBeUndefined();
    expect(getIntegration).not.toHaveBeenCalled();
  });

  it("gives up when both names are empty", async () => {
    getDeployment.mockResolvedValue({ ...DEPLOYMENT, name: "" });
    getIntegration.mockResolvedValue({ id: "i1", name: "" });
    await expect(deploymentTitle("d1")).resolves.toBeUndefined();
  });
});
