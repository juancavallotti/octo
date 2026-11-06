import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const listEnvironments = vi.fn();
const selectEnvironment = vi.fn();
vi.mock("@/app/model/environments", () => ({
  listEnvironments: () => listEnvironments(),
  selectEnvironment: (name: string) => selectEnvironment(name),
}));
vi.mock("next/navigation", () => ({ usePathname: () => "/platform/i/abc" }));

import EnvironmentSwitcher from "./EnvironmentSwitcher";
import { EnvironmentProvider, type EnvironmentContextValue } from "@/app/auth/EnvironmentContext";

const ENVIRONMENTS = [
  { name: "octo", namespace: "octo", displayName: "Default", description: "", default: true },
  { name: "staging", namespace: "octo-staging", displayName: "Staging", description: "", default: false },
];

function renderIn(value: EnvironmentContextValue) {
  return render(
    <EnvironmentProvider value={value}>
      <EnvironmentSwitcher />
    </EnvironmentProvider>,
  );
}

describe("EnvironmentSwitcher", () => {
  const assign = vi.fn();
  beforeEach(() => {
    vi.clearAllMocks();
    listEnvironments.mockResolvedValue(ENVIRONMENTS);
    selectEnvironment.mockResolvedValue(undefined);
    Object.defineProperty(window, "location", { value: { assign }, writable: true });
  });

  it("offers an administrator every environment, showing the current one", async () => {
    renderIn({ environment: "", roles: ["platform:admin"], envRoles: {} });
    const select = await screen.findByRole("combobox", { name: "Environment" });
    expect(select).toHaveProperty("value", "");
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["Default", "Staging"]);
  });

  it("switches, then lands at the top of the section", async () => {
    renderIn({ environment: "", roles: ["platform:admin"], envRoles: {} });
    await userEvent.selectOptions(await screen.findByRole("combobox"), "staging");
    await waitFor(() => expect(selectEnvironment).toHaveBeenCalledWith("staging"));
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/platform/integrations"));
  });

  it("stays out of the way of somebody with only one environment", async () => {
    renderIn({ environment: "", roles: ["platform:developer"], envRoles: {} });
    await waitFor(() => expect(listEnvironments).toHaveBeenCalled());
    expect(screen.queryByRole("combobox")).toBeNull();
  });

  it("puts a deleted choice back to the default", async () => {
    renderIn({ environment: "gone", roles: ["platform:admin"], envRoles: {} });
    await waitFor(() => expect(selectEnvironment).toHaveBeenCalledWith(""));
  });
});
