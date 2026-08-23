import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getHealth = vi.fn();
vi.mock("@/app/model/health", () => ({ getHealth: () => getHealth() }));

import PlatformServices from "./PlatformServices";
import type { Dependency } from "@/app/model/health";

const up = (name: string): Dependency => ({
  name,
  configured: true,
  reachable: true,
  latencyMs: 3,
});

describe("PlatformServices", () => {
  beforeEach(() => {
    getHealth.mockResolvedValue({
      dependencies: [up("postgres"), up("redis"), up("nats"), up("kubernetes")],
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("reports every dependency it was given", async () => {
    render(<PlatformServices />);

    await waitFor(() => expect(screen.getByText("Redis")).toBeTruthy());
    for (const label of ["Postgres", "Redis", "NATS", "Kubernetes"]) {
      expect(screen.getByText(label)).toBeTruthy();
    }
  });

  // The distinction the whole report turns on: an installation without cluster
  // access is a supported way to run, and colouring it as a fault would send
  // someone looking for one that is not there.
  it("distinguishes not configured from unreachable", async () => {
    getHealth.mockResolvedValue({
      dependencies: [
        up("postgres"),
        { name: "redis", configured: true, reachable: false, detail: "connection refused" },
        { name: "nats", configured: true, reachable: true, latencyMs: 1 },
        { name: "kubernetes", configured: false, reachable: false },
      ],
    });

    render(<PlatformServices />);

    await waitFor(() => expect(screen.getByText("Unreachable")).toBeTruthy());
    expect(screen.getByText("Not configured")).toBeTruthy();
  });

  // The transport error verbatim, because it is what someone greps for.
  it("shows why an unreachable dependency did not answer", async () => {
    getHealth.mockResolvedValue({
      dependencies: [
        { name: "redis", configured: true, reachable: false, detail: "dial tcp: i/o timeout" },
      ],
    });

    render(<PlatformServices />);

    await waitFor(() => expect(screen.getByText("dial tcp: i/o timeout")).toBeTruthy());
  });

  it("asks again on demand", async () => {
    const user = userEvent.setup();
    render(<PlatformServices />);

    await waitFor(() => expect(screen.getByText("Redis")).toBeTruthy());
    await user.click(screen.getByRole("button", { name: /Check again/ }));

    await waitFor(() => expect(getHealth).toHaveBeenCalledTimes(2));
  });

  // A report that cannot be fetched leaves nothing rather than the last one: on a
  // page whose subject is what is up right now, a stale answer is worse than none.
  it("drops the report when the orchestrator itself cannot be reached", async () => {
    const user = userEvent.setup();
    render(<PlatformServices />);
    await waitFor(() => expect(screen.getByText("Redis")).toBeTruthy());

    getHealth.mockRejectedValue(new Error("orchestrator unreachable"));
    await user.click(screen.getByRole("button", { name: /Check again/ }));

    await waitFor(() => expect(screen.getByText("orchestrator unreachable")).toBeTruthy());
    expect(screen.queryByText("Redis")).toBeNull();
  });
});

// A probe that answered in under half a millisecond rounds to 0, and reading that
// as "no measurement" would hide the fastest results — which are the ones a
// healthy in-cluster dependency actually produces.
describe("PlatformServices latency", () => {
  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("shows a zero-millisecond round trip", async () => {
    getHealth.mockResolvedValue({
      dependencies: [{ name: "redis", configured: true, reachable: true, latencyMs: 0 }],
    });

    render(<PlatformServices />);

    await waitFor(() => expect(screen.getByText(/Reachable · 0ms/)).toBeTruthy());
  });

  // No measurement at all is a different fact, and says nothing rather than 0.
  it("says nothing when no round trip was timed", async () => {
    getHealth.mockResolvedValue({
      dependencies: [{ name: "redis", configured: true, reachable: true }],
    });

    render(<PlatformServices />);

    await waitFor(() => expect(screen.getByText("Reachable")).toBeTruthy());
  });
});
