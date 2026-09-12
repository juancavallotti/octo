import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { RolesProvider } from "@/app/auth/RolesContext";
import {
  PLATFORM_ADMIN,
  PLATFORM_DEVELOPER,
  PLATFORM_MONITOR,
} from "@/app/auth/roles";
import ManagementNav from "./ManagementNav";

vi.mock("next/navigation", () => ({ usePathname: () => "/platform" }));

function renderAs(roles: string[]) {
  render(
    <RolesProvider roles={roles} mayWrite>
      <ManagementNav />
    </RolesProvider>,
  );
}

describe("the section switcher", () => {
  // Secrets is the installation's own credentials, and the orchestrator refuses
  // even the list to anyone else — so the tab would lead nowhere but a refusal.
  it("leaves Secrets out for everyone but an administrator", () => {
    renderAs([PLATFORM_DEVELOPER]);
    expect(screen.queryByRole("link", { name: "Secrets" })).toBeNull();
  });

  it("shows it to an administrator", () => {
    renderAs([PLATFORM_ADMIN]);
    expect(screen.getByRole("link", { name: "Secrets" })).toBeInTheDocument();
  });

  // Reading what is running here is not a privilege, and a monitor who arrives to
  // a nav of one tab has been told the platform is broken rather than that they
  // are a monitor.
  it("keeps every reading section for a monitor", () => {
    renderAs([PLATFORM_MONITOR]);
    for (const label of ["Dashboard", "Integrations", "Metrics", "Logs", "Traces"]) {
      expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
    }
  });
});
