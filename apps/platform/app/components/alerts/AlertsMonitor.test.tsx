import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/platform/metrics/alerts",
  useSearchParams: () => new URLSearchParams(),
}));

const listWatches = vi.fn();
const listIncidents = vi.fn();
const acknowledgeIncident = vi.fn();
vi.mock("@/app/model/alerts", () => ({
  listWatches: () => listWatches(),
  listIncidents: (...a: unknown[]) => listIncidents(...a),
  acknowledgeIncident: (...a: unknown[]) => acknowledgeIncident(...a),
}));

import AlertsMonitor from "./AlertsMonitor";
import type { Incident, WatchListItem } from "@/app/model/alerts";

function watch(over: Partial<WatchListItem["watch"]> = {}): WatchListItem {
  return {
    watch: {
      id: "w_1",
      name: "checkout errors",
      description: "",
      enabled: true,
      severity: "critical",
      combinator: "any",
      conditions: [
        {
          id: "c_1",
          type: "threshold",
          source: "traces",
          metric: "error_rate",
        },
      ],
      actions: [],
      onNoData: "ok",
      stepSeconds: 60,
      intervalSeconds: 60,
      forSeconds: 300,
      renotifySeconds: 0,
      cooldownSeconds: 0,
      ...over,
    },
    state: {
      phase: "firing",
      since: "2026-09-06T10:00:00Z",
      consecutiveFiring: 5,
      consecutiveOk: 0,
      lastEvalAt: new Date().toISOString(),
      lastStatus: "firing",
      lastValue: 0.41,
      incidentId: "i_1",
      mutedUntil: null,
      nextDueAt: null,
    },
  };
}

function incident(over: Partial<Incident> = {}): Incident {
  return {
    id: "i_1",
    watchId: "w_1",
    watchName: "checkout errors",
    openedAt: new Date(Date.now() - 20 * 60_000).toISOString(),
    resolvedAt: null,
    severity: "critical",
    acknowledgedAt: null,
    openedMatched: 1,
    openedTotal: 2,
    openedOutcomes: [],
    evaluations: 20,
    notifications: 1,
    ...over,
  };
}

describe("AlertsMonitor", () => {
  beforeEach(() => {
    listWatches.mockReset().mockResolvedValue([]);
    listIncidents.mockReset().mockResolvedValue([]);
    acknowledgeIncident.mockReset().mockResolvedValue(undefined);
  });

  it("says what to do when there is nothing yet", async () => {
    render(<AlertsMonitor />);
    expect(await screen.findByText("No watches yet")).toBeInTheDocument();
    // The empty state has to explain what a watch is for, or the button beside
    // it is an invitation with no content.
    expect(screen.getByText(/gone quiet/i)).toBeInTheDocument();
  });

  it("lists a watch with its phase and last value", async () => {
    listWatches.mockResolvedValue([watch()]);
    render(<AlertsMonitor />);

    expect(await screen.findByText("checkout errors")).toBeInTheDocument();
    expect(screen.getByText("Firing")).toBeInTheDocument();
    expect(screen.getByText("0.41")).toBeInTheDocument();
    expect(screen.getByText(/every 1m · held 5m/)).toBeInTheDocument();
  });

  // "What is wrong right now" is the first question this page is asked, and a
  // list sorted by name does not answer it.
  it("leads with what is open", async () => {
    listWatches.mockResolvedValue([watch()]);
    listIncidents.mockResolvedValue([incident()]);
    render(<AlertsMonitor />);

    expect(await screen.findByText("1 firing")).toBeInTheDocument();
    expect(screen.getByText(/1 of 2 matched/)).toBeInTheDocument();
    expect(screen.getByText(/20m so far/)).toBeInTheDocument();
    // Only open episodes: a resolved one belongs in the history, not the banner.
    expect(listIncidents).toHaveBeenCalledWith(
      expect.objectContaining({ open: true }),
    );
  });

  // An alert that fired and reached nobody is the failure worth surfacing on the
  // page rather than leaving in the history.
  it("says when an episode told nobody", async () => {
    listIncidents.mockResolvedValue([incident({ notifications: 0 })]);
    render(<AlertsMonitor />);
    expect(await screen.findByText(/nobody was told/)).toBeInTheDocument();
  });

  it("acknowledges an incident and re-reads", async () => {
    const user = userEvent.setup();
    listIncidents.mockResolvedValue([incident()]);
    render(<AlertsMonitor />);

    await user.click(
      await screen.findByRole("button", { name: "Acknowledge" }),
    );
    await waitFor(() =>
      expect(acknowledgeIncident).toHaveBeenCalledWith("i_1"),
    );
    // The list is re-read rather than patched locally, so what is on screen is
    // what the service says.
    await waitFor(() =>
      expect(listIncidents.mock.calls.length).toBeGreaterThan(1),
    );
  });

  it("does not offer to acknowledge one somebody already has", async () => {
    listIncidents.mockResolvedValue([
      incident({ acknowledgedAt: "2026-09-06T10:05:00Z" }),
    ]);
    render(<AlertsMonitor />);

    expect(await screen.findByText("Acknowledged")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Acknowledge" }),
    ).not.toBeInTheDocument();
  });

  it("surfaces a failure without emptying the page", async () => {
    listWatches.mockRejectedValue(new Error("observability is unreachable"));
    render(<AlertsMonitor />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "observability is unreachable",
    );
  });

  // A watch that is disabled or muted has to say so: both look identical to a
  // quiet one from the phase alone, and they mean quite different things.
  it("marks a disabled or muted watch", async () => {
    listWatches.mockResolvedValue([
      {
        ...watch({ enabled: false }),
        state: { ...watch().state, phase: "ok" },
      },
    ]);
    render(<AlertsMonitor />);
    expect(await screen.findByText("disabled")).toBeInTheDocument();
  });

  // A mute is only a mute while it lasts. The column is left set after one
  // expires, so a truthiness check would label a watch muted forever after
  // somebody silenced it once.
  it("only calls a watch muted while the mute lasts", async () => {
    const item = watch();
    listWatches.mockResolvedValue([
      {
        ...item,
        state: {
          ...item.state,
          mutedUntil: new Date(Date.now() + 3_600_000).toISOString(),
        },
      },
    ]);
    const { unmount } = render(<AlertsMonitor />);
    expect(await screen.findByText("muted")).toBeInTheDocument();
    unmount();

    listWatches.mockResolvedValue([
      {
        ...item,
        state: {
          ...item.state,
          mutedUntil: new Date(Date.now() - 3_600_000).toISOString(),
        },
      },
    ]);
    render(<AlertsMonitor />);
    expect(await screen.findByText("checkout errors")).toBeInTheDocument();
    expect(screen.queryByText("muted")).not.toBeInTheDocument();
  });

  // "not yet" rather than "never": a watch created a moment ago has not run, and
  // that is a different fact from one that has stopped being evaluated.
  it("distinguishes a watch that has not run from one that has", async () => {
    const item = watch();
    listWatches.mockResolvedValue([
      { ...item, state: { ...item.state, lastEvalAt: null } },
    ]);
    render(<AlertsMonitor />);
    expect(await screen.findByText("not yet")).toBeInTheDocument();
  });
});
