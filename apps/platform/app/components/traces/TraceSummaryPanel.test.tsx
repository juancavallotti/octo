import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import TraceSummaryPanel from "./TraceSummaryPanel";
import { buildWaterfall } from "./buildWaterfall";
import { records } from "./fixtures";
import type { TraceSummary } from "@/app/model/traces";

/**
 * The panel is where a reader gets the numbers they will quote to someone else,
 * so the two things it must not do are overstate a cost and imply that the time
 * breakdown is a share of a whole when it isn't.
 */

const SUMMARY: TraceSummary = {
  traceId: "tr-1",
  deploymentId: "dep-1",
  integrationId: "int-1",
  appName: "Orders",
  appVersion: "v1.0",
  deploymentIds: ["dep-1"],
  startedAt: "2026-08-09T10:00:00.000Z",
  endedAt: "2026-08-09T10:00:01.000Z",
  rootFlow: "handle-order",
  entryKind: "source.receive",
  entryLabel: "POST /orders",
  status: "ok",
  rootDurationNs: 1_000_000_000,
  records: 24,
  llmCalls: 2,
  inputTokens: 1800,
  outputTokens: 260,
  cachedTokens: 0,
  costUsd: 0.0094,
  unpricedCalls: 0,
  models: ["claude-sonnet-4"],
};

/** A sequential trace: one REST call, then a transform. */
function sequential() {
  return buildWaterfall(
    records(
      { kind: "block.post-invoke", start: 0, duration: 600, path: "orders.call", blockType: "rest" },
      { kind: "block.post-invoke", start: 600, duration: 200, path: "orders.shape", blockType: "multi-transform" },
      { kind: "flow.completed", start: 0, duration: 1_000, flow: "orders" },
    ),
  );
}

/** A fork running a REST call and a transform at the same time. */
function concurrent() {
  return buildWaterfall(
    records(
      { kind: "block.post-invoke", start: 0, duration: 400, path: "orders.fan[0].call", blockType: "rest" },
      { kind: "block.post-invoke", start: 0, duration: 400, path: "orders.fan[1].shape", blockType: "multi-transform" },
      { kind: "block.post-invoke", start: 0, duration: 400, path: "orders.fan", blockType: "fork" },
      { kind: "flow.completed", start: 0, duration: 1_000, flow: "orders" },
    ),
  );
}

describe("TraceSummaryPanel", () => {
  /** The figure on the bar labelled `label`. */
  const bar = (label: string) =>
    screen.getByText(label).parentElement?.textContent?.replace(label, "").trim();

  it("splits the time between waiting and working", () => {
    render(<TraceSummaryPanel summary={SUMMARY} waterfall={sequential()} />);
    expect(bar("Waiting")).toBe("600ns");
    expect(bar("Working")).toBe("200ns");
  });

  it("shows what no block accounted for rather than absorbing it", () => {
    // 200ns of the 1000 was engine overhead. Left implicit, the other bars would
    // quietly grow to cover it.
    render(<TraceSummaryPanel summary={SUMMARY} waterfall={sequential()} />);
    expect(bar("Unattributed")).toBe("200ns");
  });

  it("says when the bars overlap, so they are not read as shares", () => {
    // 400ns of waiting and 400ns of working over 400ns of wall clock. Both are
    // true; a stacked bar drawn from them would have to be wrong somewhere.
    render(<TraceSummaryPanel summary={SUMMARY} waterfall={concurrent()} />);
    expect(screen.getByText(/add up to more than the trace lasted/)).toBeInTheDocument();
  });

  it("does not claim an overlap when there was none", () => {
    render(<TraceSummaryPanel summary={SUMMARY} waterfall={sequential()} />);
    expect(screen.queryByText(/add up to more than/)).not.toBeInTheDocument();
  });

  it("names the blocks it could not classify", () => {
    const unknown = buildWaterfall(
      records(
        { kind: "block.post-invoke", start: 0, duration: 500, path: "orders.x", blockType: "brand-new-block" },
        { kind: "flow.completed", start: 0, duration: 1_000, flow: "orders" },
      ),
    );
    render(<TraceSummaryPanel summary={SUMMARY} waterfall={unknown} />);
    expect(screen.getByText(/Unclassified: brand-new-block/)).toBeInTheDocument();
  });

  it("reads its aggregates from the stored rollup", () => {
    // Not recomputed from the records — that is what keeps this number and the
    // one in the trace list from drifting apart.
    render(<TraceSummaryPanel summary={SUMMARY} waterfall={sequential()} />);
    expect(screen.getByText("24")).toBeInTheDocument(); // records rolled up
    expect(screen.getByText("2")).toBeInTheDocument(); // model calls
    expect(screen.getByText("2060")).toBeInTheDocument(); // 1800 in + 260 out
    expect(screen.getByText("$0.0094")).toBeInTheDocument();
  });

  it("marks a partly priced trace as a lower bound", () => {
    render(
      <TraceSummaryPanel
        summary={{ ...SUMMARY, unpricedCalls: 1 }}
        waterfall={sequential()}
      />,
    );
    expect(screen.getByText("≥ $0.0094")).toBeInTheDocument();
  });

  it("shows no cost row at all when nothing called a model", () => {
    render(
      <TraceSummaryPanel
        summary={{ ...SUMMARY, llmCalls: 0, costUsd: 0, models: [] }}
        waterfall={sequential()}
      />,
    );
    expect(screen.queryByText("Cost")).not.toBeInTheDocument();
    expect(screen.queryByText("Model calls")).not.toBeInTheDocument();
  });

  it("says when a trace was not one app's story", () => {
    render(
      <TraceSummaryPanel
        summary={{ ...SUMMARY, deploymentIds: ["dep-1", "dep-2"] }}
        waterfall={sequential()}
      />,
    );
    expect(screen.getByText("Deployments")).toBeInTheDocument();
  });
});
