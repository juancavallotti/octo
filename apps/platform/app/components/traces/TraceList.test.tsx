import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TraceList from "./TraceList";
import type { TraceSummary } from "@/app/model/traces";

/**
 * The list's job is to be scannable and not to overstate anything. Two claims
 * are easy to get wrong here: how long a trace took, and what it cost.
 */

const TRACE: TraceSummary = {
  traceId: "tr-1",
  deploymentId: "dep-1",
  integrationId: "int-1",
  appName: "Orders",
  appVersion: "v1.0",
  deploymentIds: ["dep-1"],
  startedAt: "2026-08-09T10:00:00.000Z",
  endedAt: "2026-08-09T10:00:01.500Z",
  rootFlow: "handle-order",
  entryKind: "source.receive",
  entryLabel: "POST /orders",
  status: "ok",
  rootDurationNs: 1_400_000_000,
  records: 24,
  llmCalls: 0,
  inputTokens: 0,
  outputTokens: 0,
  cachedTokens: 0,
  costUsd: 0,
  unpricedCalls: 0,
  models: [],
};

function renderList(traces: TraceSummary[], over: Partial<Parameters<typeof TraceList>[0]> = {}) {
  const onSelect = vi.fn();
  const onLoadMore = vi.fn();
  render(
    <TraceList
      traces={traces}
      selectedId={null}
      loading={false}
      loadingMore={false}
      hasMore={false}
      onSelect={onSelect}
      onLoadMore={onLoadMore}
      {...over}
    />,
  );
  return { onSelect, onLoadMore };
}

describe("TraceList", () => {
  it("shows the whole span rather than the root flow's own duration", () => {
    // The span is defined for every trace, including one whose terminal record
    // was lost, and it is what the waterfall draws. Showing the root's duration
    // would give the same trace two different lengths in two panes.
    renderList([TRACE]);
    expect(screen.getByText("1.5s")).toBeInTheDocument();
    expect(screen.queryByText("1.4s")).not.toBeInTheDocument();
  });

  it("leads with how the caller reached the flow", () => {
    renderList([TRACE]);
    expect(screen.getByText("POST /orders")).toBeInTheDocument();
    expect(screen.getByText("handle-order")).toBeInTheDocument();
  });

  it("falls back to the flow when there is no entry label", () => {
    renderList([{ ...TRACE, entryLabel: "" }]);
    expect(screen.getByText("handle-order")).toBeInTheDocument();
  });

  it("falls back to the id when it knows neither", () => {
    renderList([{ ...TRACE, traceId: "tr-2", entryLabel: "", rootFlow: "" }]);
    expect(screen.getByText("tr-2")).toBeInTheDocument();
  });

  it("marks a failure so it can be found by scanning", () => {
    renderList([{ ...TRACE, status: "failed" }]);
    expect(screen.getByLabelText("failed")).toBeInTheDocument();
  });

  it("shows no cost at all for a trace that called no model", () => {
    // Not "$0" — a trace with no model calls has no cost to report, and a dollar
    // figure invites comparing it with one that does.
    renderList([TRACE]);
    expect(screen.queryByText("$0")).not.toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("never shows an unpriced trace as free", () => {
    renderList([{ ...TRACE, llmCalls: 2, unpricedCalls: 2, models: ["some-new-model"] }]);
    expect(screen.getByText("unpriced")).toBeInTheDocument();
    expect(screen.queryByText("$0")).not.toBeInTheDocument();
  });

  it("names the models behind a call count", () => {
    renderList([{ ...TRACE, llmCalls: 3, costUsd: 0.02, models: ["claude-sonnet-4"] }]);
    expect(screen.getByTitle("claude-sonnet-4")).toHaveTextContent("3");
  });

  it("says when a trace crossed more than one deployment", () => {
    // The id rides on the message and survives a queue hop, so this is how a
    // reader learns the trace is not one app's story.
    renderList([{ ...TRACE, deploymentIds: ["dep-1", "dep-2"] }]);
    expect(screen.getByTitle(/crossed 2 deployments/)).toBeInTheDocument();
  });

  it("offers an older page when the service handed back a cursor", async () => {
    const { onLoadMore } = renderList([TRACE], { hasMore: true });
    await userEvent.click(screen.getByRole("button", { name: "Load older" }));
    expect(onLoadMore).toHaveBeenCalled();
  });

  it("offers no older page on the last one", () => {
    // The cursor being absent is the service saying there is nothing beyond
    // this, so a button here would page into an empty answer.
    renderList([TRACE]);
    expect(screen.queryByRole("button", { name: "Load older" })).not.toBeInTheDocument();
  });

  it("says it is still loading rather than that there is nothing", () => {
    renderList([], { loading: true });
    expect(screen.getByText("Loading traces…")).toBeInTheDocument();
  });

  it("says there is nothing once there is nothing", () => {
    renderList([]);
    expect(screen.getByText(/no trace matches these filters/i)).toBeInTheDocument();
  });

  it("hands back the trace that was clicked", async () => {
    const { onSelect } = renderList([TRACE, { ...TRACE, traceId: "tr-2", entryLabel: "GET /health" }]);
    await userEvent.click(screen.getByText("GET /health"));
    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ traceId: "tr-2" }));
  });
});
