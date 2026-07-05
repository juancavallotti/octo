import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// A save capability that resolves immediately; the id is read from the ref passed to
// the component, so save itself is a no-op here.
const save = vi.fn().mockResolvedValue(undefined);
vi.mock("@octo/editor", () => ({
  useSave: () => ({ save, empty: false }),
}));

const getIntegration = vi.fn();
const listDeployments = vi.fn();
const listSnapshots = vi.fn();
const createSnapshot = vi.fn();
const rolloutDeployment = vi.fn();
const createDeployment = vi.fn();
const getDeployOptions = vi.fn();
vi.mock("@/app/model/orchestrator", () => ({
  getIntegration: (id: string) => getIntegration(id),
  listDeployments: (id: string) => listDeployments(id),
  listSnapshots: (id: string) => listSnapshots(id),
  createSnapshot: (id: string, tag: string) => createSnapshot(id, tag),
  rolloutDeployment: (id: string, snap: string, env?: unknown) =>
    rolloutDeployment(id, snap, env),
  createDeployment: (id: string, input: unknown) => createDeployment(id, input),
  getDeployOptions: (id: string, opts?: unknown) => getDeployOptions(id, opts),
}));
vi.mock("@/app/model/secrets", () => ({ listSecrets: () => Promise.resolve([]) }));

import DeployButton from "./DeployButton";

const DEPLOYMENT = {
  id: "dep-1",
  integrationId: "int-1",
  name: "Orders",
  tag: "v1.0",
  status: "running",
  replicas: 1,
  readyReplicas: 1,
  desiredReplicas: 1,
  lastUpdated: "",
  env: {},
};

describe("DeployButton (editor)", () => {
  beforeEach(() => {
    getIntegration.mockResolvedValue({ id: "int-1", name: "Orders" });
    listSnapshots.mockResolvedValue([
      { id: "snap-1", integrationId: "int-1", tag: "v1.0", definition: "", createdAt: "" },
    ]);
    createSnapshot.mockResolvedValue({ id: "snap-2", tag: "v1.1" });
    rolloutDeployment.mockResolvedValue({});
    getDeployOptions.mockResolvedValue({
      networked: false,
      slugValid: false,
      slugAvailable: false,
      envVars: [],
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it("opens the rollout dialog and upgrades the chosen deployment", async () => {
    listDeployments.mockResolvedValue([DEPLOYMENT]);
    render(<DeployButton getIntegrationId={() => "int-1"} />);

    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));

    // The rollout dialog opens (new-tag mode: a version input, no Scale field).
    expect(await screen.findByText("New version")).toBeInTheDocument();
    expect(screen.queryByText("Scale")).not.toBeInTheDocument();
    await waitFor(() =>
      expect(getDeployOptions).toHaveBeenCalledWith("int-1", {}),
    );

    // The dialog's Deploy button tags the working copy and rolls out that deployment.
    const dialog = screen.getByRole("dialog");
    const dialogDeploy = within(dialog).getByRole("button", { name: "Deploy" });
    await waitFor(() => expect(dialogDeploy).toBeEnabled());
    await userEvent.click(dialogDeploy);

    await waitFor(() =>
      expect(createSnapshot).toHaveBeenCalledWith("int-1", expect.any(String)),
    );
    expect(rolloutDeployment).toHaveBeenCalledWith("dep-1", "snap-2", {});
    expect(createDeployment).not.toHaveBeenCalled();
  });

  it("opens the first-deploy modal when nothing is live", async () => {
    listDeployments.mockResolvedValue([]);
    render(<DeployButton getIntegrationId={() => "int-1"} />);

    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));

    // The first-deploy modal (Current mode) shows the Scale field; no rollout happens.
    expect(await screen.findByText("Scale")).toBeInTheDocument();
    expect(rolloutDeployment).not.toHaveBeenCalled();
  });
});
