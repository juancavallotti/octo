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

// A fixed-version deploy (a tag is active), so the modal shows the version rather
// than a picker and submits its snapshot id.
const SNAPSHOT = {
  id: "snap-1",
  integrationId: "int-1",
  tag: "v1.0",
  definition: "",
  createdAt: "",
};

function renderModal(onSubmit = vi.fn()) {
  render(
    <DeployModal
      integrationId="int-1"
      integrationName="Orders"
      activeSnapshot={SNAPSHOT}
      snapshots={[SNAPSHOT]}
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
    // API_KEY also appears in the missing-required hint, so it matches more than
    // once; LOG_LEVEL (optional) appears only as its field label.
    expect((await screen.findAllByText("API_KEY")).length).toBeGreaterThan(0);
    expect(screen.getByText("LOG_LEVEL")).toBeInTheDocument();
  });

  it("blocks deploy until a required var is filled, then submits a secret binding", async () => {
    const onSubmit = renderModal();
    await screen.findAllByText("API_KEY");

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

describe("DeployModal tracing", () => {
  beforeEach(() => {
    // No declared env vars, so nothing blocks the deploy button.
    getDeployOptions.mockResolvedValue({
      networked: false,
      slugValid: false,
      slugAvailable: false,
    });
    listSecrets.mockResolvedValue([]);
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("is off by default and sends no tracing field", async () => {
    const onSubmit = renderModal();
    const toggle = await screen.findByRole("checkbox", {
      name: /trace this deployment/i,
    });
    expect(toggle).not.toBeChecked();

    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));
    // Absent rather than false: the orchestrator reads an omitted field as
    // "leave it alone", and tracing is a thing you ask for, never a default.
    expect(onSubmit).toHaveBeenCalledWith({
      snapshotId: "snap-1",
      replicas: 1,
    });
  });

  it("submits tracing and warns about captured payloads once on", async () => {
    const onSubmit = renderModal();
    const toggle = await screen.findByRole("checkbox", {
      name: /trace this deployment/i,
    });
    expect(screen.queryByText(/carry credentials/i)).not.toBeInTheDocument();

    await userEvent.click(toggle);
    expect(await screen.findByText(/carry credentials/i)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));
    expect(onSubmit).toHaveBeenCalledWith({
      snapshotId: "snap-1",
      replicas: 1,
      tracing: true,
    });
  });
});
