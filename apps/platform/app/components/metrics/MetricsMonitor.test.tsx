import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

/**
 * The three ways this page has nothing to draw, which are three different
 * problems with three different fixes:
 *
 *   the service is unreachable   an operator sets LOGS_URL
 *   no pod ever reported         an operator turns the sidecar on
 *   no rows in this window       the reader picks a wider range
 *
 * Collapsing them into one message would send every reader to the wrong one, so
 * they are pinned here rather than left to whoever edits the component next.
 */

const listStatsPods = vi.fn();
const readStatsSeries = vi.fn();
vi.mock("@/app/model/stats", () => ({
  listStatsPods: (id: string) => listStatsPods(id),
  readStatsSeries: (id: string, q: unknown) => readStatsSeries(id, q),
}));

const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  usePathname: () => "/platform/metrics/dep-1",
  useSearchParams: () => new URLSearchParams(),
}));

import MetricsMonitor from "./MetricsMonitor";

const NOW = 1_757_000_000_000;

function seriesPage(over: Record<string, unknown> = {}) {
  return {
    deploymentId: "dep-1",
    tier: "live",
    step: "1s",
    from: new Date(NOW - 300_000).toISOString(),
    to: new Date(NOW).toISOString(),
    series: [],
    warnings: [],
    truncated: false,
    ...over,
  };
}

function pod(over: Record<string, unknown> = {}) {
  return {
    pod: "octo-dep-1-abc",
    lastSeen: new Date(NOW).toISOString(),
    reporting: true,
    startedAt: null,
    sampleInterval: "1s",
    rollupInterval: "1h0m0s",
    retention: "168h0m0s",
    generation: 1,
    series: 95,
    liveRows: 3600,
    rollupRows: 168,
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  listStatsPods.mockResolvedValue({ deploymentId: "dep-1", items: [], truncated: false });
  readStatsSeries.mockResolvedValue(seriesPage());
});

describe("when there is nothing to draw", () => {
  it("names the environment variable when the service is unreachable", async () => {
    readStatsSeries.mockRejectedValue(new Error("pod stats not configured (LOGS_URL unset)"));

    render(<MetricsMonitor deploymentId="dep-1" />);

    await screen.findByText("Metrics unavailable");
    expect(screen.getByText(/Set LOGS_URL to enable it/)).toBeInTheDocument();
    // The raw message shows too: the empty state says what to do, the strip says
    // what happened.
    expect(screen.getByText(/pod stats not configured/)).toBeInTheDocument();
  });

  it("points at the chart setting when no pod has ever reported", async () => {
    render(<MetricsMonitor deploymentId="dep-1" />);

    await screen.findByText("No pod stats for this deployment");
    expect(screen.getByText(/orchestrator\.podStats/)).toBeInTheDocument();
  });

  it("suggests a wider range when pods report but stored nothing here", async () => {
    listStatsPods.mockResolvedValue({
      deploymentId: "dep-1",
      items: [pod()],
      truncated: false,
    });
    readStatsSeries.mockResolvedValue(
      seriesPage({ warnings: [{ pod: "octo-dep-1-abc", reason: "no rows in window" }] }),
    );

    render(<MetricsMonitor deploymentId="dep-1" />);

    await screen.findByText("Nothing recorded in this window");
    // The service's own reason is shown rather than summarized away.
    expect(screen.getByText(/no rows in window/)).toBeInTheDocument();
  });
});

describe("the window", () => {
  it("asks for both process metrics against the resolvable tier", async () => {
    render(<MetricsMonitor deploymentId="dep-1" />);

    await waitFor(() => expect(readStatsSeries).toHaveBeenCalled());
    const query = readStatsSeries.mock.calls[0][1];
    expect(query.metrics).toEqual([
      "process_cpu_seconds_total",
      "process_resident_memory_bytes",
    ]);
    expect(query.tier).toBe("auto");
  });

  it("puts the range in the URL rather than in state", async () => {
    // A window worth showing somebody should be a link, and Back should walk
    // through the ranges the reader actually looked at.
    render(<MetricsMonitor deploymentId="dep-1" />);
    await waitFor(() => expect(readStatsSeries).toHaveBeenCalled());

    await userEvent.click(screen.getByRole("button", { name: "24h" }));

    expect(replace).toHaveBeenCalledWith("/platform/metrics/dep-1?range=24h", {
      scroll: false,
    });
  });

  it("reports which tier answered and at what resolution", async () => {
    listStatsPods.mockResolvedValue({
      deploymentId: "dep-1",
      items: [pod()],
      truncated: false,
    });
    readStatsSeries.mockResolvedValue(seriesPage({ tier: "rollup", step: "1h0m0s" }));

    render(<MetricsMonitor deploymentId="dep-1" />);

    // Without this line a 24h view made of 24 points looks like missing data.
    await screen.findByText(/history · one point per 1h · 1 pod/);
  });

  it("says so when the pod list was capped", async () => {
    readStatsSeries.mockResolvedValue(seriesPage({ truncated: true }));

    render(<MetricsMonitor deploymentId="dep-1" />);

    await screen.findByText(/part of the picture/);
  });
});
