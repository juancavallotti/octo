import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const listEnvironments = vi.fn();
const selectEnvironment = vi.fn();
const promoteSnapshot = vi.fn();
vi.mock("@/app/model/environments", () => ({
  listEnvironments: () => listEnvironments(),
  selectEnvironment: (name: string) => selectEnvironment(name),
}));
vi.mock("@/app/model/orchestrator", () => ({
  promoteSnapshot: (id: string, environment: string) => promoteSnapshot(id, environment),
}));

import PromoteButton from "./PromoteButton";
import { EnvironmentProvider } from "@/app/auth/EnvironmentContext";

const ENVIRONMENTS = [
  { name: "octo", namespace: "octo", displayName: "Default", description: "", default: true },
  { name: "staging", namespace: "octo-staging", displayName: "Staging", description: "", default: false },
];
const V1 = { id: "snap-1", integrationId: "int-1", tag: "v1.0.0", createdAt: "2026-11-01T00:00:00Z" };

function renderWith(snapshot: typeof V1 | null) {
  return render(
    <EnvironmentProvider value={{ environment: "", roles: ["platform:admin"], envRoles: {} }}>
      <PromoteButton snapshot={snapshot as never} busy={false} />
    </EnvironmentProvider>,
  );
}

describe("PromoteButton", () => {
  const assign = vi.fn();
  beforeEach(() => {
    vi.clearAllMocks();
    listEnvironments.mockResolvedValue(ENVIRONMENTS);
    selectEnvironment.mockResolvedValue(undefined);
    Object.defineProperty(window, "location", { value: { assign }, writable: true });
  });

  it("asks for a version when the working copy is in view", async () => {
    renderWith(null);
    const button = await screen.findByRole("button", { name: /Promote/ });
    expect(button).toHaveProperty("disabled", true);
    expect(button.getAttribute("title")).toBe("Pick a version to promote");
  });

  it("promotes the version in view, then offers to open it there", async () => {
    promoteSnapshot.mockResolvedValue({
      integrationId: "int-9",
      created: true,
      environment: "staging",
      snapshot: { ...V1, id: "snap-9", integrationId: "int-9" },
    });
    const person = userEvent.setup();
    renderWith(V1);

    await person.click(await screen.findByRole("button", { name: /Promote/ }));
    await person.click(screen.getByRole("menuitem", { name: /Staging/ }));

    await waitFor(() => expect(promoteSnapshot).toHaveBeenCalledWith("snap-1", "staging"));
    expect((await screen.findByRole("status")).textContent).toMatch(/v1.0.0 is now in Staging, as a new integration/);

    await person.click(screen.getByRole("button", { name: /Open it in Staging/ }));
    await waitFor(() => expect(selectEnvironment).toHaveBeenCalledWith("staging"));
    expect(assign).toHaveBeenCalledWith("/platform/integrations/i/int-9");
  });

  it("shows the orchestrator's refusal", async () => {
    promoteSnapshot.mockRejectedValue(new Error("a snapshot with this tag already exists"));
    const person = userEvent.setup();
    renderWith(V1);
    await person.click(await screen.findByRole("button", { name: /Promote/ }));
    await person.click(screen.getByRole("menuitem", { name: /Staging/ }));
    expect((await screen.findByRole("alert")).textContent).toMatch(/already exists/);
  });

  it("stays out of the way when there is nowhere to promote to", async () => {
    listEnvironments.mockResolvedValue([ENVIRONMENTS[0]]);
    renderWith(V1);
    await waitFor(() => expect(listEnvironments).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: /Promote/ })).toBeNull();
  });
});
