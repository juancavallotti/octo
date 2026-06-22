import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getDeployOptions = vi.fn();
const listSecrets = vi.fn();
vi.mock("@/app/model/orchestrator", () => ({
  getDeployOptions: () => getDeployOptions(),
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

    // Switch API_KEY (first row) to Secret mode and pick the cluster secret.
    await userEvent.click(screen.getAllByRole("button", { name: "secret" })[0]);
    await userEvent.selectOptions(
      await screen.findByRole("combobox"),
      "DB_PASSWORD",
    );

    await waitFor(() => expect(deploy).toBeEnabled());
    await userEvent.click(deploy);

    expect(onSubmit).toHaveBeenCalledWith({
      replicas: 1,
      env: { API_KEY: { secret: "DB_PASSWORD" } },
    });
  });
});
