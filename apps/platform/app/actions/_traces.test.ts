import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The trace-store client, at the two places it can quietly lie.
 *
 * The first is cost. The trace store is careful to distinguish "this call cost
 * nothing" from "nobody could say what this call cost" — a null token count, a
 * null cost and a `cost_status` explaining which. That distinction survives the
 * database, the query and the JSON, and this layer is the last place it can be
 * thrown away by a `?? 0` that looks like defensive coding. A call priced at $0
 * is a claim about a bill; an unpriced one is an admission.
 *
 * The second is the captured payloads. Traces carry the actual messages a flow
 * handled, and the temptation in a snake_case→camelCase mapper is to walk the
 * whole document. That would rewrite the user's own keys, so the trace would show
 * something the flow never carried.
 */

const requestJson = vi.fn();
vi.mock("@octo/http", () => ({
  requestJson: (method: string, url: string) => requestJson(method, url),
}));

import * as traces from "./_traces";
import type { RawRecord, RawSummary } from "./_tracesWire";

const BASE = "http://logs:8091";

/** The URL the single mocked call was made against. */
function calledUrl(): string {
  expect(requestJson).toHaveBeenCalledTimes(1);
  return requestJson.mock.calls[0][1] as string;
}

/** Its query string, as a lookup that keeps repeated keys. */
function calledParams(): URLSearchParams {
  return new URL(calledUrl()).searchParams;
}

function ok<T>(data: T) {
  requestJson.mockResolvedValue({ ok: true, data });
}

/** A summary with every field present, so a test can override just its subject. */
const SUMMARY: RawSummary = {
  trace_id: "tr-1",
  deployment_id: "dep-1",
  integration_id: "int-1",
  app_name: "Orders",
  app_version: "v1.0",
  deployment_ids: ["dep-1"],
  started_at: "2026-08-09T10:00:00Z",
  ended_at: "2026-08-09T10:00:01Z",
  root_flow: "handle-order",
  entry_kind: "source.receive",
  entry_label: "POST /orders",
  status: "ok",
  root_duration_ns: 1_000_000_000,
  records: 12,
  llm_calls: 1,
  input_tokens: 400,
  output_tokens: 120,
  cached_tokens: 0,
  cost_usd: 0,
  unpriced_calls: 1,
  models: ["some-new-model"],
};

/** A model call the catalogue had no rate for: usage known, price unknowable. */
const UNPRICED: RawRecord = {
  id: "rec-1",
  trace_id: "tr-1",
  seq: 7,
  kind: "llm.turn",
  event_id: "ev-1",
  correlation_id: "corr-1",
  flow: "handle-order",
  path: "handle-order[main].agent",
  block_type: "ai-agent",
  ts: "2026-08-09T10:00:01Z",
  duration_ns: 900_000_000,
  error: "",
  dropped: false,
  truncated: false,
  body: null,
  vars: null,
  attrs: { connector: "my-llm" },
  deployment_id: "dep-1",
  app_name: "Orders",
  app_version: "v1.0",
  model: "some-new-model",
  provider: "",
  input_tokens: 400,
  output_tokens: 120,
  thinking_tokens: 40,
  cached_tokens: null,
  cache_write_tokens: null,
  cost_usd: null,
  cost_status: "unpriced_model",
  price_id: "",
};

describe("the trace-store client", () => {
  beforeEach(() => {
    process.env.LOGS_URL = BASE;
  });

  afterEach(() => {
    vi.clearAllMocks();
    delete process.env.LOGS_URL;
  });

  it("keeps an unpriced call unpriced instead of free", async () => {
    ok({ summary: SUMMARY, items: [UNPRICED], truncated: false });

    const res = await traces.getTrace("tr-1", true);
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    const record = res.data.items[0];
    // The whole point: null, and specifically not zero. A reader shown $0.00 for
    // this call would conclude the model was free rather than unknown.
    expect(record.costUsd).toBeNull();
    expect(record.costUsd).not.toBe(0);
    expect(record.costStatus).toBe("unpriced_model");
    // Same rule for a count the provider never reported.
    expect(record.cachedTokens).toBeNull();
    // A count that *was* reported still comes through as itself.
    expect(record.inputTokens).toBe(400);
    expect(record.thinkingTokens).toBe(40);
  });

  it("carries the unpriced count beside a summary's total", async () => {
    ok({ summary: SUMMARY, items: [], truncated: false });

    const res = await traces.getTrace("tr-1", true);
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    // A summary's cost is a sum, so it is 0 rather than null here. That is only
    // honest next to the count that says the 0 is a lower bound — drop the count
    // and this trace reads as a model call that cost nothing.
    expect(res.data.summary.costUsd).toBe(0);
    expect(res.data.summary.unpricedCalls).toBe(1);
    expect(res.data.summary.llmCalls).toBe(1);
  });

  it("passes captured payloads through without renaming their keys", async () => {
    const body = { user_id: 42, line_items: [{ sku_id: "a-1" }] };
    ok({
      summary: SUMMARY,
      items: [{ ...UNPRICED, body, vars: { request_id: "r-9" } }],
      truncated: false,
    });

    const res = await traces.getTrace("tr-1", true);
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    // The user's keys are the user's. Only the fields we own are renamed.
    expect(res.data.items[0].body).toEqual(body);
    expect(res.data.items[0].vars).toEqual({ request_id: "r-9" });
  });

  it("keeps a missing payload missing rather than empty", async () => {
    ok({ summary: SUMMARY, items: [UNPRICED], truncated: true });

    const res = await traces.getTrace("tr-1", false);
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    // `{}` would claim the flow carried an empty document.
    expect(res.data.items[0].body).toBeNull();
    expect(res.data.items[0].vars).toBeNull();
    // Truncation is reported, not swallowed: a waterfall drawn from a cut record
    // set is wrong in a way the picture cannot show.
    expect(res.data.truncated).toBe(true);
    // Bodies default to on at the service, so only the declining case is sent.
    expect(calledParams().get("bodies")).toBe("0");
  });

  it("asks for bodies by saying nothing", async () => {
    ok({ summary: SUMMARY, items: [], truncated: false });
    await traces.getTrace("tr-1", true);
    expect(calledUrl()).toBe(`${BASE}/traces/tr-1`);
  });

  it("percent-encodes a trace id into the path", async () => {
    ok({ summary: SUMMARY, items: [], truncated: false });
    await traces.getTrace("tr/1 2", true);
    // Concatenated raw, this would address a different route entirely.
    expect(calledUrl()).toBe(`${BASE}/traces/tr%2F1%202`);
  });

  it("sends every filter it was given and nothing it was not", async () => {
    ok({ items: [SUMMARY], next_before: "2026-08-09T10:00:00Z|tr-1" });

    const res = await traces.getTraces({
      deploymentId: "dep-1",
      statuses: ["failed", "dropped"],
      minDurationMs: 250,
      hasLlm: true,
      q: "orders",
      before: "2026-08-09T11:00:00Z|tr-9",
      limit: 50,
    });
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    const params = calledParams();
    expect(params.getAll("status")).toEqual(["failed", "dropped"]);
    expect(params.get("minDurationMs")).toBe("250");
    expect(params.get("hasLlm")).toBe("true");
    // The cursor is opaque and round-trips verbatim, separator included.
    expect(params.get("before")).toBe("2026-08-09T11:00:00Z|tr-9");
    // Axes nobody constrained are absent, not empty — an empty appName would
    // filter for apps literally named "".
    expect(params.has("appName")).toBe(false);
    expect(params.has("flow")).toBe(false);
    expect(res.data.nextBefore).toBe("2026-08-09T10:00:00Z|tr-1");
  });

  it("omits hasLlm rather than sending it false", async () => {
    ok({ items: [] });
    await traces.getTraces({ hasLlm: false });
    // The service reads the parameter as a flag; "hasLlm=false" would filter
    // nothing while reading, to anyone tracing the request, as if it did.
    expect(calledParams().has("hasLlm")).toBe(false);
  });

  it("reports the last page as the last page", async () => {
    ok({ items: [SUMMARY] });
    const res = await traces.getTraces({});
    expect(res.ok).toBe(true);
    if (!res.ok) return;
    expect(res.data.nextBefore).toBeNull();
  });

  it("survives a service that answers with a null list", async () => {
    ok({ items: null, from: "2026-08-09T00:00:00Z", to: "2026-08-09T12:00:00Z" });
    const res = await traces.getApps({});
    expect(res.ok).toBe(true);
    if (!res.ok) return;
    // Go marshals a nil slice as null; the window is echoed back either way,
    // because every count in the response is meaningless without it.
    expect(res.data.items).toEqual([]);
    expect(res.data.from).toBe("2026-08-09T00:00:00Z");
  });

  it("says so when the trace service is unconfigured", async () => {
    delete process.env.LOGS_URL;
    const res = await traces.getTraces({});
    expect(res).toEqual({
      ok: false,
      error: "trace query not configured (LOGS_URL unset)",
    });
    // No request is attempted against a base URL of "".
    expect(requestJson).not.toHaveBeenCalled();
  });

  it("passes a failed request through untouched", async () => {
    requestJson.mockResolvedValue({ ok: false, error: "connection refused" });
    const res = await traces.getTrace("tr-1", true);
    expect(res).toEqual({ ok: false, error: "connection refused" });
  });
});
