import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getAgentStatus = vi.fn();
const installAgent = vi.fn();
const rolloutAgent = vi.fn();
const setAgentTracing = vi.fn();
const setAgentMaxIterations = vi.fn();
const uninstallAgent = vi.fn();
vi.mock("@/app/model/agent", () => ({
  getAgentStatus: () => getAgentStatus(),
  installAgent: () => installAgent(),
  rolloutAgent: () => rolloutAgent(),
  setAgentTracing: (on: boolean) => setAgentTracing(on),
  setAgentMaxIterations: (n: number) => setAgentMaxIterations(n),
  uninstallAgent: (purge: boolean) => uninstallAgent(purge),
}));

import AgentSettingsManager from "./AgentSettingsManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import type { AgentStatus } from "@/app/model/agent";

const NOT_INSTALLED: AgentStatus = {
  state: "not_installed",
  updateAvailable: false,
  edited: false,
  tracing: false,
};

const DEPLOYED: AgentStatus = {
  state: "deployed",
  integrationId: "int-1",
  deploymentId: "dep-1",
  internalUrl: "http://octo-int-dr-octo:8080",
  installedTag: "v0.8.1",
  updateAvailable: false,
  edited: false,
  tracing: false,
  deploymentStatus: "running",
};

function renderManager() {
  return render(
    <ConfirmProvider>
      <AgentSettingsManager />
    </ConfirmProvider>,
  );
}

/**
 * Press the confirm dialog's affirmative button. Scoped to the dialog because it
 * shares its label with the page button that opened it.
 */
async function confirmDialog(user: ReturnType<typeof userEvent.setup>, label: RegExp) {
  await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: label }));
}

describe("AgentSettingsManager", () => {
  beforeEach(() => {
    getAgentStatus.mockResolvedValue(NOT_INSTALLED);
    installAgent.mockResolvedValue(DEPLOYED);
    rolloutAgent.mockResolvedValue(DEPLOYED);
    setAgentTracing.mockResolvedValue({ ...DEPLOYED, tracing: true });
    setAgentMaxIterations.mockResolvedValue({ ...DEPLOYED, maxIterations: 40 });
    uninstallAgent.mockResolvedValue(undefined);
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("offers Install when nothing is installed, and nothing else", async () => {
    renderManager();

    await waitFor(() => expect(screen.getByRole("button", { name: "Install" })).toBeTruthy());
    expect(screen.queryByRole("button", { name: /tracing/i })).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove the agent" })).toBeNull();
  });

  // Every button on this page is a decision about what is already installed, so a
  // click before the first load resolves would be acting on a guess.
  it("does not install before the status has loaded", async () => {
    const user = userEvent.setup();
    let release: (s: AgentStatus) => void = () => {};
    getAgentStatus.mockReturnValue(
      new Promise<AgentStatus>((resolve) => {
        release = resolve;
      }),
    );
    renderManager();

    expect(screen.queryByRole("button", { name: "Install" })).toBeNull();
    expect(screen.getByText(/Loading/)).toBeTruthy();

    release(NOT_INSTALLED);
    await waitFor(() => expect(screen.getByRole("button", { name: "Install" })).toBeTruthy());
    await user.click(screen.getByRole("button", { name: "Install" }));
    expect(installAgent).toHaveBeenCalledTimes(1);
  });

  // A blocked install is shown with its reason rather than hidden: the button being
  // there and disabled is what makes the reason worth reading.
  it("disables Install and explains why when something blocks it", async () => {
    getAgentStatus.mockResolvedValue({ ...NOT_INSTALLED, blocked: "llm_key" });
    renderManager();

    await waitFor(() =>
      expect((screen.getByRole("button", { name: "Install" }) as HTMLButtonElement).disabled).toBe(
        true,
      ),
    );
    expect(screen.getByRole("link", { name: /Configure the LLM provider/ }).getAttribute("href")).toBe(
      "/platform/admin/llm",
    );
  });

  it("offers Deploy, not Install, when the integration exists but nothing runs", async () => {
    getAgentStatus.mockResolvedValue({
      ...DEPLOYED,
      state: "installed",
      deploymentId: undefined,
      internalUrl: undefined,
      deploymentStatus: undefined,
    });
    renderManager();

    await waitFor(() => expect(screen.getByRole("button", { name: "Deploy" })).toBeTruthy());
    expect(screen.queryByRole("button", { name: "Install" })).toBeNull();
    // Tracing is a rolling update of a deployment that does not exist yet.
    expect(screen.queryByRole("button", { name: /tracing/i })).toBeNull();
  });

  it("toggles tracing to the opposite of what is running", async () => {
    const user = userEvent.setup();
    getAgentStatus.mockResolvedValue({ ...DEPLOYED, tracing: true });
    renderManager();

    await waitFor(() => expect(screen.getByRole("button", { name: /Turn tracing off/ })).toBeTruthy());
    await user.click(screen.getByRole("button", { name: /Turn tracing off/ }));

    await waitFor(() => expect(setAgentTracing).toHaveBeenCalledWith(false));
  });

  // The headline risk of rolling out: an edited agent is replaced by the shipped
  // one. Saying so is the whole point of tracking `edited` — and saying that the
  // edits are frozen as their own version first is what makes it a warning rather
  // than a dead end.
  it("warns that a roll-out replaces local edits when the agent was edited", async () => {
    const user = userEvent.setup();
    getAgentStatus.mockResolvedValue({
      ...DEPLOYED,
      state: "update_available",
      updateAvailable: true,
      edited: true,
    });
    renderManager();

    await waitFor(() => expect(screen.getByRole("button", { name: "Roll out update" })).toBeTruthy());
    await user.click(screen.getByRole("button", { name: "Roll out update" }));

    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    expect(screen.getByText(/frozen as its own version/)).toBeTruthy();
    expect(screen.getByText(/agent-edited-/)).toBeTruthy();

    await confirmDialog(user, /^Roll out$/);
    await waitFor(() => expect(rolloutAgent).toHaveBeenCalledTimes(1));
  });

  // Without this the only way back to the shipped agent was for the bundle's digest
  // to move: an agent edited into a state you wanted to undo had no button, because
  // "no update available" hid the one that would have fixed it.
  it("offers a reinstall when the agent is current, not just when an update exists", async () => {
    const user = userEvent.setup();
    getAgentStatus.mockResolvedValue(DEPLOYED);
    renderManager();

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Reinstall from stock" })).toBeTruthy(),
    );
    await user.click(screen.getByRole("button", { name: "Reinstall from stock" }));

    await confirmDialog(user, /^Reinstall$/);
    await waitFor(() => expect(rolloutAgent).toHaveBeenCalledTimes(1));
  });

  it("does not warn about edits when there are none", async () => {
    const user = userEvent.setup();
    getAgentStatus.mockResolvedValue({
      ...DEPLOYED,
      state: "update_available",
      updateAvailable: true,
    });
    renderManager();

    await waitFor(() => expect(screen.getByRole("button", { name: "Roll out update" })).toBeTruthy());
    await user.click(screen.getByRole("button", { name: "Roll out update" }));

    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    expect(screen.queryByText(/will be replaced/)).toBeNull();
  });

  // Keeping the integration is what makes an edited agent survive a removal, so the
  // default must not purge.
  it("removes the deployment but keeps the integration", async () => {
    const user = userEvent.setup();
    getAgentStatus.mockResolvedValue(DEPLOYED);
    renderManager();

    await waitFor(() => expect(screen.getByRole("button", { name: "Remove the agent" })).toBeTruthy());
    await user.click(screen.getByRole("button", { name: "Remove the agent" }));
    await confirmDialog(user, /^Remove$/);

    await waitFor(() => expect(uninstallAgent).toHaveBeenCalledWith(false));
  });

  // A failed load leaves nothing on screen to act on, so it must not sit on
  // "Loading…" forever next to an error message that contradicts it.
  it("offers a retry when the status cannot be read, rather than loading forever", async () => {
    const user = userEvent.setup();
    getAgentStatus.mockRejectedValueOnce(new Error("orchestrator unreachable"));
    renderManager();

    await waitFor(() => expect(screen.getByText("orchestrator unreachable")).toBeTruthy());
    expect(screen.queryByText(/Loading/)).toBeNull();

    getAgentStatus.mockResolvedValue(DEPLOYED);
    await user.click(screen.getByRole("button", { name: "Try again" }));

    await waitFor(() => expect(screen.getByRole("button", { name: "Remove the agent" })).toBeTruthy());
    expect(screen.queryByText("orchestrator unreachable")).toBeNull();
  });

  // The nastier half of the same bug: the action worked, so the card on screen
  // describes a state that no longer exists, and its buttons would act on it.
  it("does not keep a stale status when the refresh after an action fails", async () => {
    const user = userEvent.setup();
    getAgentStatus.mockResolvedValueOnce(NOT_INSTALLED);
    renderManager();

    await waitFor(() => expect(screen.getByRole("button", { name: "Install" })).toBeTruthy());
    getAgentStatus.mockRejectedValueOnce(new Error("orchestrator unreachable"));
    await user.click(screen.getByRole("button", { name: "Install" }));

    await waitFor(() => expect(screen.getByText("orchestrator unreachable")).toBeTruthy());
    // The agent was installed; offering to install it again would be acting on a
    // status the page knows it could not read.
    expect(screen.queryByRole("button", { name: "Install" })).toBeNull();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });

  it("surfaces a failed deployment's reason and offers a redeploy", async () => {
    getAgentStatus.mockResolvedValue({
      ...DEPLOYED,
      state: "failed",
      deploymentStatus: "failed",
      reason: "ImagePullBackOff",
    });
    renderManager();

    await waitFor(() => expect(screen.getByText("ImagePullBackOff")).toBeTruthy());
    expect(screen.getByRole("button", { name: "Redeploy" })).toBeTruthy();
  });
});

/**
 * The turn limit — the only edited setting on this page, and the one with a rule
 * worth pinning: an empty field is not "unchanged", it is how the override is
 * cleared.
 */
describe("AgentSettingsManager turn limit", () => {
  beforeEach(() => {
    getAgentStatus.mockResolvedValue(DEPLOYED);
    setAgentMaxIterations.mockResolvedValue({ ...DEPLOYED, maxIterations: 40 });
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  /** The field, once the page has loaded a deployed agent. */
  async function turnLimit() {
    renderManager();
    return waitFor(() => screen.getByLabelText("Turn limit") as HTMLInputElement);
  }

  // Nothing to configure when nothing is running: the setting reaches the runtime
  // by replacing pods, and there are none.
  it("is hidden until the agent is deployed", async () => {
    getAgentStatus.mockResolvedValue(NOT_INSTALLED);
    renderManager();

    await waitFor(() => expect(screen.getByRole("button", { name: "Install" })).toBeTruthy());
    expect(screen.queryByLabelText("Turn limit")).toBeNull();
  });

  // Empty rather than a number, because no override is in force and showing the
  // definition's own default here would claim one that does not exist.
  it("starts empty when the definition's default is in force", async () => {
    const field = await turnLimit();

    expect(field.value).toBe("");
    expect(screen.getByRole("button", { name: "Apply" }).hasAttribute("disabled")).toBe(true);
  });

  it("applies a limit that is in range", async () => {
    const user = userEvent.setup();
    const field = await turnLimit();

    await user.type(field, "40");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(setAgentMaxIterations).toHaveBeenCalledWith(40));
  });

  // Clearing the field is the only way back to the shipped default, so it has to
  // reach the orchestrator as the zero that means "no override" rather than as a
  // no-op the page quietly swallows.
  it("clears the override by emptying the field", async () => {
    const user = userEvent.setup();
    getAgentStatus.mockResolvedValue({ ...DEPLOYED, maxIterations: 40 });
    setAgentMaxIterations.mockResolvedValue(DEPLOYED);
    const field = await turnLimit();
    expect(field.value).toBe("40");

    await user.clear(field);
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(setAgentMaxIterations).toHaveBeenCalledWith(0));
  });

  // Answered here rather than by the orchestrator, because the round trip that
  // would answer it also replaces the agent's pods.
  it("refuses a limit outside the range without asking the server", async () => {
    const user = userEvent.setup();
    const field = await turnLimit();

    await user.type(field, "500");

    expect(screen.getByText(/Between 1 and 200/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Apply" }).hasAttribute("disabled")).toBe(true);
    expect(setAgentMaxIterations).not.toHaveBeenCalled();
  });
});
