import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
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
  { name: "staging", namespace: "octo-staging", displayName: "Staging", description: "Pre-production", default: false },
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

  it("shows the current environment's display name on the chip", async () => {
    renderIn({ environment: "staging", roles: ["platform:admin"], envRoles: {} });
    expect((await screen.findByRole("button", { name: "Environment" })).textContent).toContain("Staging");
  });

  it("lists each environment with its name, namespace and description, the current one checked", async () => {
    const person = userEvent.setup();
    renderIn({ environment: "", roles: ["platform:admin"], envRoles: {} });
    await person.click(await screen.findByRole("button", { name: "Environment" }));

    const menu = screen.getByRole("menu", { name: "Environments" });
    const items = within(menu).getAllByRole("menuitemradio");
    expect(items).toHaveLength(2);
    expect(items[0].getAttribute("aria-checked")).toBe("true");
    expect(items[1].textContent).toContain("staging · octo-staging");
    expect(items[1].textContent).toContain("Pre-production");
    expect(within(menu).getByRole("link", { name: /Manage environments/ }).getAttribute("href")).toBe(
      "/platform/admin/environments",
    );
  });

  it("switches, then lands at the top of the section", async () => {
    const person = userEvent.setup();
    renderIn({ environment: "", roles: ["platform:admin"], envRoles: {} });
    await person.click(await screen.findByRole("button", { name: "Environment" }));
    await person.click(screen.getByRole("menuitemradio", { name: /Staging/ }));
    await waitFor(() => expect(selectEnvironment).toHaveBeenCalledWith("staging"));
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/platform/integrations"));
  });

  it("offers no management link to anybody but an administrator", async () => {
    const person = userEvent.setup();
    renderIn({ environment: "", roles: ["platform:developer"], envRoles: { staging: ["platform:developer"] } });
    await person.click(await screen.findByRole("button", { name: "Environment" }));
    expect(screen.queryByRole("link", { name: /Manage environments/ })).toBeNull();
  });

  it("stays out of the way of somebody with only one environment", async () => {
    renderIn({ environment: "", roles: ["platform:developer"], envRoles: {} });
    await waitFor(() => expect(listEnvironments).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "Environment" })).toBeNull();
  });

  it("shows the way to the one environment somebody holds a role in, from the default", async () => {
    renderIn({ environment: "", roles: [], envRoles: { staging: ["platform:developer"] } });
    await userEvent.click(await screen.findByRole("button", { name: "Environment" }));
    expect(screen.getByRole("menuitemradio", { name: /Staging/ })).toBeTruthy();
  });

  it("names the chosen environment when the list cannot be read", async () => {
    listEnvironments.mockRejectedValue(new Error("down"));
    renderIn({ environment: "staging", roles: ["platform:developer"], envRoles: { staging: ["platform:developer"] } });
    expect((await screen.findByRole("button", { name: "Environment" })).textContent).toContain("staging");
  });

  it("says a refused switch was refused", async () => {
    selectEnvironment.mockRejectedValue(new Error("environment not found"));
    renderIn({ environment: "", roles: ["platform:admin"], envRoles: {} });
    await userEvent.click(await screen.findByRole("button", { name: "Environment" }));
    await userEvent.click(screen.getByRole("menuitemradio", { name: /Staging/ }));
    expect((await screen.findByRole("alert")).getAttribute("title")).toBe("environment not found");
    expect(assign).not.toHaveBeenCalled();
  });

  it("puts a deleted choice back to the default", async () => {
    renderIn({ environment: "gone", roles: ["platform:admin"], envRoles: {} });
    await waitFor(() => expect(selectEnvironment).toHaveBeenCalledWith(""));
  });
});
