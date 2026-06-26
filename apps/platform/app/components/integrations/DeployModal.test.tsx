import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getDeployOptions = vi.fn();
const listSnapshots = vi.fn();
const listSecrets = vi.fn();
vi.mock("@/app/model/orchestrator", () => ({
  getDeployOptions: () => getDeployOptions(),
  listSnapshots: () => listSnapshots(),
}));
vi.mock("@/app/model/secrets", () => ({ listSecrets: () => listSecrets() }));

import DeployModal from "./DeployModal";

function renderModal(onSubmit = vi.fn()) {
  render(
    <DeployModal
      integrationId="int-1"
      integrationName="Orders"
      busy={false}
      error={null}
      onSubmit={onSubmit}
      onClose={vi.fn()}
    />,
  );
  return onSubmit;
}

describe("DeployModal environment section", () => {
  beforeEach(() => {
    // A non-networked integration (no slug/expose UI) declaring two env vars.
    getDeployOptions.mockResolvedValue({
      networked: false,
      slugValid: false,
      slugAvailable: false,
      envVars: [
        { name: "API_KEY", required: true },
        { name: "LOG_LEVEL", default: "info" },
      ],
    });
    listSnapshots.mockResolvedValue([
      { id: "snap-1", integrationId: "int-1", tag: "v1.0", createdAt: "" },
    ]);
    listSecrets.mockResolvedValue([
      { name: "DB_PASSWORD", createdAt: "", lastUpdated: "" },
    ]);
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("renders a row per declared env var", async () => {
    renderModal();
    expect(await screen.findByText("API_KEY")).toBeInTheDocument();
    expect(screen.getByText("LOG_LEVEL")).toBeInTheDocument();
  });

  it("blocks deploy until a required var is filled, then submits a secret binding", async () => {
    const onSubmit = renderModal();
    await screen.findByText("API_KEY");

    const deploy = screen.getByRole("button", { name: "Deploy" });
    expect(deploy).toBeDisabled(); // API_KEY is required and unset

    // Switch API_KEY (first row) to Secret mode and pick the cluster secret. There
    // are now two comboboxes (the Version selector and the secret picker); the
    // secret picker is the one that just appeared.
    await userEvent.click(screen.getAllByRole("button", { name: "secret" })[0]);
    const combos = await screen.findAllByRole("combobox");
    await userEvent.selectOptions(combos[combos.length - 1], "DB_PASSWORD");

    await waitFor(() => expect(deploy).toBeEnabled());
    await userEvent.click(deploy);

    expect(onSubmit).toHaveBeenCalledWith({
      snapshotId: "snap-1",
      replicas: 1,
      env: { API_KEY: { secret: "DB_PASSWORD" } },
    });
  });
});
