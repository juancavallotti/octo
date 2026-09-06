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
  it("names the tier rather than letting the service choose", async () => {
    // auto resolves a window longer than the live tier reaches entirely to
    // buckets, which turns a hundred and twenty points into two without saying
    // so. The view is the tier, so there is nothing to resolve.
    render(<MetricsMonitor deploymentId="dep-1" />);

    await waitFor(() => expect(readStatsSeries).toHaveBeenCalled());
    const query = readStatsSeries.mock.calls[0][1];
    expect(query.metrics).toEqual([
      "process_cpu_seconds_total",
      "process_resident_memory_bytes",
    ]);
    expect(query.tier).toBe("live");
    expect(query.tier).not.toBe("auto");
  });

  it("puts the view in the URL rather than in state", async () => {
    // A view worth showing somebody should be a link.
    render(<MetricsMonitor deploymentId="dep-1" />);
    await waitFor(() => expect(readStatsSeries).toHaveBeenCalled());

    await userEvent.click(screen.getByRole("button", { name: "Historic" }));

    expect(replace).toHaveBeenCalledWith("/platform/metrics/dep-1?view=historic", {
      scroll: false,
    });
  });

  it("navigates to the historic view rather than holding it in state", async () => {
    replace.mockClear();
    render(<MetricsMonitor deploymentId="dep-1" />);
    await waitFor(() => expect(readStatsSeries).toHaveBeenCalled());
    readStatsSeries.mockClear();

    await userEvent.click(screen.getByRole("button", { name: "Historic" }));

    // The URL drives the view, and the test's useSearchParams is fixed, so this
    // asserts the intent that was navigated to rather than a re-render.
    expect(replace).toHaveBeenCalledWith("/platform/metrics/dep-1?view=historic", {
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

    // Without this line a view made of a handful of points looks like missing
    // data rather than like the resolution the tier holds.
    await screen.findByText(/history · one point per 1h/);
  });

  it("says so when the pod list was capped", async () => {
    readStatsSeries.mockResolvedValue(seriesPage({ truncated: true }));

    render(<MetricsMonitor deploymentId="dep-1" />);

    await screen.findByText(/part of the picture/);
  });
});

describe("when only one of the two calls fails", () => {
  it("keeps the chart when the pod list is what failed", async () => {
    // Promise.all would discard a perfectly good series response because its
    // neighbour rejected, blanking the chart the page exists to draw.
    listStatsPods.mockRejectedValue(new Error("pods unavailable"));
    readStatsSeries.mockResolvedValue(
      seriesPage({
        series: [
          {
            pod: "octo-dep-1-abc",
            name: "process_resident_memory_bytes",
            kind: "gauge",
            labels: {},
            times: [NOW - 1000, NOW],
            ends: [],
            values: [1e8, 1.1e8],
            min: [],
            max: [],
            last: [],
            samples: [],
          },
        ],
      }),
    );

    render(<MetricsMonitor deploymentId="dep-1" />);

    // The failure is reported, and the data that arrived is still on screen.
    await screen.findByText(/pods unavailable/);
    expect(screen.queryByText("Metrics unavailable")).not.toBeInTheDocument();
    expect(screen.queryByText("No pod stats for this deployment")).not.toBeInTheDocument();
  });

  it("still reports an error when both fail", async () => {
    listStatsPods.mockRejectedValue(new Error("pods unavailable"));
    readStatsSeries.mockRejectedValue(new Error("series unavailable"));

    render(<MetricsMonitor deploymentId="dep-1" />);

    await screen.findByText("Metrics unavailable");
  });
});
