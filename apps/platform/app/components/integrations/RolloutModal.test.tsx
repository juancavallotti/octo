import type { ComponentProps } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getDeployOptions = vi.fn();
const listSecrets = vi.fn();
vi.mock("@/app/model/orchestrator", () => ({
  getDeployOptions: (id: string, opts?: unknown) => getDeployOptions(id, opts),
}));
vi.mock("@/app/model/secrets", () => ({ listSecrets: () => listSecrets() }));

import RolloutModal from "./RolloutModal";
import type { Deployment, Snapshot } from "@/app/model/orchestrator";

const SNAPSHOT: Snapshot = {
  id: "snap-1",
  integrationId: "int-1",
  tag: "v1.0",
  definition: "",
  createdAt: "",
};

// A running deployment of v1.0 that already binds API_KEY to a literal value.
const DEPLOYMENT: Deployment = {
  id: "dep-1",
  integrationId: "int-1",
  name: "Orders",
  tag: "v1.0",
  status: "running",
  replicas: 1,
  readyReplicas: 1,
  desiredReplicas: 1,
  lastUpdated: "",
  env: { API_KEY: { value: "seeded" } },
};

function renderModal(
  onSubmit = vi.fn(),
  overrides: Partial<ComponentProps<typeof RolloutModal>> = {},
) {
  render(
    <RolloutModal
      integrationId="int-1"
      integrationName="Orders"
      deployments={[DEPLOYMENT]}
      snapshots={[SNAPSHOT]}
      deployedTags={new Set(["v1.0"])}
      versionMode="pick"
      busy={false}
      error={null}
      onSubmit={onSubmit}
      onClose={vi.fn()}
      {...overrides}
    />,
  );
  return onSubmit;
}

describe("RolloutModal", () => {
  beforeEach(() => {
    // API_KEY has no default (placeholder ""), LOG_LEVEL has one (placeholder
    // "default: info") — so the API_KEY input is the sole empty-placeholder field.
    getDeployOptions.mockResolvedValue({
      networked: false,
      slugValid: false,
      slugAvailable: false,
      envVars: [
        { name: "API_KEY", required: true },
        { name: "LOG_LEVEL", default: "info" },
      ],
    });
    listSecrets.mockResolvedValue([]);
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("seeds env from the deployment and rolls out to the current tag", async () => {
    const onSubmit = renderModal();
    // API_KEY seeded from the deployment's binding, so its field carries the value
    // and the required var is already satisfied.
    expect(
      (await screen.findByDisplayValue("seeded")) as HTMLInputElement,
    ).toBeInTheDocument();

    // Options were fetched for the deployment's current tag (its frozen snapshot).
    await waitFor(() =>
      expect(getDeployOptions).toHaveBeenCalledWith("int-1", {
        snapshotId: "snap-1",
      }),
    );

    const roll = screen.getByRole("button", { name: "Roll out" });
    await waitFor(() => expect(roll).toBeEnabled());
    await userEvent.click(roll);

    expect(onSubmit).toHaveBeenCalledWith({
      deploymentId: "dep-1",
      snapshotId: "snap-1",
      env: { API_KEY: { value: "seeded" } },
    });
  });

  it("blocks the roll out until a required var is filled", async () => {
    const onSubmit = renderModal(vi.fn(), {
      deployments: [{ ...DEPLOYMENT, env: {} }],
    });
    const roll = await screen.findByRole("button", { name: "Roll out" });
    expect(roll).toBeDisabled(); // API_KEY required and unset

    const apiKey = screen.getByPlaceholderText("") as HTMLInputElement;
    await userEvent.type(apiKey, "abc123");

    await waitFor(() => expect(roll).toBeEnabled());
    await userEvent.click(roll);
    expect(onSubmit).toHaveBeenCalledWith({
      deploymentId: "dep-1",
      snapshotId: "snap-1",
      env: { API_KEY: { value: "abc123" } },
    });
  });

  it("cuts a new tag from the working copy in new-tag mode", async () => {
    const onSubmit = renderModal(vi.fn(), { versionMode: "new-tag" });

    const tag = await screen.findByPlaceholderText("e.g. v1.0.0");
    await userEvent.clear(tag);
    await userEvent.type(tag, "v2.0.0");

    // New-tag mode reads options from the live definition (no snapshotId).
    await waitFor(() => expect(getDeployOptions).toHaveBeenCalledWith("int-1", {}));

    const roll = screen.getByRole("button", { name: "Roll out" });
    await waitFor(() => expect(roll).toBeEnabled());
    await userEvent.click(roll);
    expect(onSubmit).toHaveBeenCalledWith({
      deploymentId: "dep-1",
      newTag: "v2.0.0",
      env: { API_KEY: { value: "seeded" } },
    });
  });
});
