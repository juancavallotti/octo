import { beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";

let pathname = "/platform/integrations";
let search = "";
vi.mock("next/navigation", () => ({
  usePathname: () => pathname,
  useSearchParams: () => new URLSearchParams(search),
}));

import EnvironmentInUrl from "./EnvironmentInUrl";
import { EnvironmentProvider } from "@/app/auth/EnvironmentContext";

function show(environment: string) {
  window.history.replaceState(null, "", pathname + (search ? `?${search}` : ""));
  render(
    <EnvironmentProvider value={{ environment, roles: [], envRoles: {} }}>
      <EnvironmentInUrl />
    </EnvironmentProvider>,
  );
  return new URL(window.location.href);
}

describe("EnvironmentInUrl", () => {
  beforeEach(() => {
    pathname = "/platform/integrations";
    search = "";
  });

  it("names the environment a page is in, so it can be bookmarked", () => {
    expect(show("staging").searchParams.get("env")).toBe("staging");
  });

  it("names nothing in the default", () => {
    search = "env=octo";
    expect(show("").searchParams.has("env")).toBe(false);
  });

  it("corrects a link to an environment the page is not in", () => {
    search = "env=prod";
    expect(show("staging").searchParams.get("env")).toBe("staging");
  });

  it("keeps the admin section's addresses free of an environment", () => {
    pathname = "/platform/admin/agent";
    search = "env=staging";
    expect(show("staging").searchParams.has("env")).toBe(false);
  });
});
