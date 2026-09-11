import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { RolesProvider, useRoles } from "./RolesContext";
import { PLATFORM_ADMIN, PLATFORM_MONITOR } from "./roles";

function Probe() {
  const { has, isAdmin, roles } = useRoles();
  return (
    <div>
      <span data-testid="admin">{String(isAdmin)}</span>
      <span data-testid="monitor">{String(has(PLATFORM_MONITOR))}</span>
      <span data-testid="roles">{roles.join(",")}</span>
    </div>
  );
}

function renderWith(roles: string[]) {
  render(
    <RolesProvider roles={roles}>
      <Probe />
    </RolesProvider>,
  );
}

describe("useRoles", () => {
  it("reports the roles the caller holds", () => {
    renderWith([PLATFORM_MONITOR]);
    expect(screen.getByTestId("monitor").textContent).toBe("true");
    expect(screen.getByTestId("admin").textContent).toBe("false");
    expect(screen.getByTestId("roles").textContent).toBe(PLATFORM_MONITOR);
  });

  it("reports admin for a caller who holds it", () => {
    renderWith([PLATFORM_ADMIN]);
    expect(screen.getByTestId("admin").textContent).toBe("true");
  });

  it("holds nothing for a caller granted nothing", () => {
    renderWith([]);
    expect(screen.getByTestId("admin").textContent).toBe("false");
    expect(screen.getByTestId("monitor").textContent).toBe("false");
  });

  it("throws when used outside its provider", () => {
    expect(() => render(<Probe />)).toThrow(/within a RolesProvider/);
  });
});
