import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The retention client, at the places a mapper can quietly change what was said.
 *
 * Retention is the one setting where zero is a value rather than an absence: it
 * means "keep this stream forever". So every `?? 0`-shaped reflex in a mapping
 * layer is a hazard here, and so is the reverse — a null cutoff in a sweep's
 * report is what distinguishes an axis nobody asked to prune from one where
 * nothing had expired, and both come back with zero deleted.
 *
 * It also talks to the aggregator rather than the orchestrator, so the URL it
 * builds is worth pinning: sent to the wrong service these routes 404, and sent
 * with no service at all they must say so rather than fail as a bare fetch.
 */

const requestJson = vi.fn();
vi.mock("@octo/http", () => ({
  requestJson: (method: string, url: string, body?: unknown) =>
    requestJson(method, url, body),
}));

import * as retention from "./_retention";

const BASE = "http://observability:8091";

function ok(data: unknown) {
  requestJson.mockResolvedValue({ ok: true, data });
}

describe("the retention client", () => {
  beforeEach(() => {
    process.env.OBSERVABILITY_URL = BASE;
  });

  afterEach(() => {
    vi.clearAllMocks();
    delete process.env.OBSERVABILITY_URL;
  });

  it("reads the policy from the aggregator", async () => {
    ok({ logs_days: 30, traces_days: 7, updated_at: "2026-08-11T03:00:00Z" });

    const res = await retention.getRetention();
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    expect(requestJson.mock.calls[0][0]).toBe("GET");
    expect(requestJson.mock.calls[0][1]).toBe(`${BASE}/settings/retention`);
    expect(res.data).toEqual({
      logsDays: 30,
      tracesDays: 7,
      updatedAt: "2026-08-11T03:00:00Z",
    });
  });

  // Zero is a policy, not a missing field. A mapper that treated it as absent
  // would turn "keep everything" into whatever it defaulted to.
  it("carries a zero window through as a zero", async () => {
    ok({ logs_days: 0, traces_days: 0, updated_at: null });

    const res = await retention.getRetention();
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    expect(res.data.logsDays).toBe(0);
    expect(res.data.tracesDays).toBe(0);
    expect(res.data.updatedAt).toBeNull();
  });

  it("sends both windows on a save, in the wire's casing", async () => {
    ok({ logs_days: 30, traces_days: 0, updated_at: "2026-08-11T03:00:00Z" });

    await retention.saveRetention({ logsDays: 30, tracesDays: 0 });

    expect(requestJson.mock.calls[0][0]).toBe("PUT");
    expect(requestJson.mock.calls[0][1]).toBe(`${BASE}/settings/retention`);
    // Both fields, and traces_days present rather than dropped for being zero:
    // the service refuses a partial policy precisely so this cannot happen.
    expect(requestJson.mock.calls[0][2]).toEqual({ logs_days: 30, traces_days: 0 });
  });

  it("posts a sweep and maps what it deleted", async () => {
    ok({
      logs_deleted: 120,
      traces_deleted: 44,
      trace_summaries_deleted: 6,
      logs_cutoff: "2026-07-12T03:00:00Z",
      traces_cutoff: null,
      duration_ms: 1500,
    });

    const res = await retention.runRetention();
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    expect(requestJson.mock.calls[0][0]).toBe("POST");
    expect(requestJson.mock.calls[0][1]).toBe(`${BASE}/retention/run`);
    expect(res.data.logsDeleted).toBe(120);
    expect(res.data.traceSummariesDeleted).toBe(6);
    // Null, and specifically not a date: this axis was set to keep everything,
    // which is why it deleted nothing. Substituting a timestamp would say the
    // opposite — that a sweep ran against it and found nothing.
    expect(res.data.tracesCutoff).toBeNull();
  });

  it("trims a trailing slash off the configured address", async () => {
    process.env.OBSERVABILITY_URL = `${BASE}/`;
    ok({ logs_days: 0, traces_days: 0, updated_at: null });

    await retention.getRetention();

    expect(requestJson.mock.calls[0][1]).toBe(`${BASE}/settings/retention`);
  });

  it("says so when the aggregator's address is unset", async () => {
    delete process.env.OBSERVABILITY_URL;

    for (const call of [
      retention.getRetention(),
      retention.saveRetention({ logsDays: 1, tracesDays: 1 }),
      retention.runRetention(),
    ]) {
      const res = await call;
      expect(res.ok).toBe(false);
      if (res.ok) return;
      expect(res.error).toMatch(/OBSERVABILITY_URL/);
    }
    expect(requestJson).not.toHaveBeenCalled();
  });
});
