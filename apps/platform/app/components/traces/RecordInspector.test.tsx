import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

/**
 * The inspector is the one place a reader sees what a flow actually carried, so
 * the distinctions it has to keep are about *absence*: a payload that was never
 * captured, one that could not be attributed to this invocation, and a call
 * nobody could price all look alike once rendered badly.
 */

const getTraceRecord = vi.fn();
vi.mock("@/app/model/traces", () => ({
  getTraceRecord: (traceId: string, id: string) => getTraceRecord(traceId, id),
}));

import RecordInspector from "./RecordInspector";
import { record } from "./fixtures";
import type { WaterfallNode } from "./types";

function node(over: Partial<WaterfallNode> = {}): WaterfallNode {
  const rec = record({ kind: "block.post-invoke", path: "orders.call", blockType: "rest" });
  return {
    id: rec.id,
    kind: rec.kind,
    label: "call",
    path: rec.path,
    eventId: rec.eventId,
    start: 0,
    end: 1000,
    durationNs: 1000,
    seq: rec.seq,
    record: rec,
    input: null,
    inputMatched: true,
    inferred: false,
    lane: 0,
    depth: 0,
    children: [],
    ...over,
  };
}

function show(over: Partial<WaterfallNode> = {}) {
  const shown = node(over);
  render(<RecordInspector traceId="tr-1" node={shown} onClose={vi.fn()} />);
  return shown;
}

describe("RecordInspector", () => {
  beforeEach(() => {
    getTraceRecord.mockResolvedValue(record({ kind: "block.post-invoke" }));
  });
  afterEach(() => vi.clearAllMocks());

  it("fetches the payloads the trace was loaded without", async () => {
    // Ids come off the fixture counter, so the assertion reads the one it made
    // rather than a literal that drifts with test order.
    const shown = show();
    await waitFor(() =>
      expect(getTraceRecord).toHaveBeenCalledWith("tr-1", shown.record!.id),
    );
  });

  it("shows a captured body as it was carried", async () => {
    const body = { user_id: 42, items: [{ sku_id: "a-1" }] };
    getTraceRecord.mockResolvedValue(record({ kind: "block.post-invoke", body }));
    show();
    await waitFor(() =>
      expect(screen.getByText(/"user_id": 42/)).toBeInTheDocument(),
    );
  });

  it("says a payload was never captured rather than showing an empty one", async () => {
    show();
    await waitFor(() =>
      expect(screen.getByText(/No payload was captured/)).toBeInTheDocument(),
    );
  });

  it("refuses to show an input it could not attribute", async () => {
    // Under a fork the runtime is explicit that pre/post pairing is unreliable.
    show({ inputMatched: false });
    expect(screen.getByText(/ran concurrently with itself/)).toBeInTheDocument();
  });

  it("explains why a model call could not be priced", async () => {
    const call = record({ kind: "llm.turn", path: "orders.agent" });
    show({
      kind: "llm.turn",
      record: { ...call, model: "some-new-model", costStatus: "unpriced_model", costUsd: null },
    });
    await waitFor(() =>
      expect(screen.getByText(/not in the price catalogue/)).toBeInTheDocument(),
    );
    // And never as a price of nothing.
    expect(screen.queryByText("$0")).not.toBeInTheDocument();
  });

  it("does not add thinking tokens to the output count", async () => {
    const call = record({ kind: "llm.turn" });
    show({
      kind: "llm.turn",
      record: { ...call, inputTokens: 100, outputTokens: 400, thinkingTokens: 350, costStatus: "priced", costUsd: 0.01 },
    });
    // 400 out, of which 350 was thinking — not 750.
    await waitFor(() =>
      expect(screen.getByText(/400 out · 350 of it thinking/)).toBeInTheDocument(),
    );
  });

  it("says when a span's extent was inferred rather than measured", () => {
    show({ inferred: true, record: null });
    expect(screen.getByText(/a lower bound on what really happened/)).toBeInTheDocument();
  });
});
