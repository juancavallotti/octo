import { beforeEach, describe, expect, it, vi } from "vitest";

const requestJson = vi.fn();
vi.mock("@octo/http", () => ({
  requestJson: (...a: unknown[]) => requestJson(...a),
}));

const BASE = "http://observability:8091";
vi.mock("./_observability", () => ({
  observabilityBaseUrl: () => BASE,
  observabilityUnconfigured: () => ({ ok: false, error: "unconfigured" }),
}));

import * as alerts from "./_alerts";

/** Stand in for a 2xx carrying `body`. */
function ok(body: unknown) {
  requestJson.mockResolvedValue({ ok: true, data: body });
}

const rawWatch = {
  id: "w_1",
  name: "checkout errors",
  description: "",
  enabled: true,
  severity: "warning",
  combinator: "any",
  conditions: [
    { id: "c_1", type: "threshold", source: "traces", metric: "error_rate" },
  ],
  actions: [{ id: "a_1", type: "email", params: { to: ["ops@example.com"] } }],
  on_no_data: "ok",
  step_seconds: 60,
  interval_seconds: 60,
  for_seconds: 300,
  created_at: "2026-09-06T10:00:00Z",
  updated_at: null,
};

describe("the alerting client", () => {
  beforeEach(() => {
    requestJson.mockReset();
  });

  it("maps a watch out of the wire's casing", async () => {
    ok({
      items: [
        {
          watch: rawWatch,
          state: {
            phase: "firing",
            since: null,
            consecutive_firing: 3,
            consecutive_ok: 0,
            last_eval_at: null,
            last_status: "firing",
            last_value: 0.41,
            incident_id: "i_1",
            muted_until: null,
            next_due_at: null,
          },
        },
      ],
    });

    const res = await alerts.listWatches();
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    const [item] = res.data;
    expect(item.watch.onNoData).toBe("ok");
    expect(item.watch.intervalSeconds).toBe(60);
    expect(item.state.phase).toBe("firing");
    expect(item.state.incidentId).toBe("i_1");
    // The conditions cross as the shapes the service stores, untouched.
    expect(item.watch.conditions[0].metric).toBe("error_rate");
  });

  // A watch with no conditions yet is a real state in the editor, and a mapper
  // that assumed an array would throw on it rather than render an empty form.
  it("tolerates absent collections", async () => {
    ok({
      items: [
        {
          watch: { ...rawWatch, conditions: null, actions: null },
          state: {
            phase: "ok",
            since: null,
            consecutive_firing: 0,
            consecutive_ok: 0,
            last_eval_at: null,
            last_status: "",
            last_value: null,
            muted_until: null,
            next_due_at: null,
          },
        },
      ],
    });

    const res = await alerts.listWatches();
    expect(res.ok).toBe(true);
    if (!res.ok) return;
    expect(res.data[0].watch.conditions).toEqual([]);
    expect(res.data[0].watch.actions).toEqual([]);
  });

  it("sends a save in the wire's casing, with the acting user", async () => {
    ok(rawWatch);

    await alerts.saveWatch(
      "w 1",
      {
        name: "checkout errors",
        description: "",
        enabled: true,
        severity: "warning",
        combinator: "any",
        conditions: [],
        actions: [],
        onNoData: "ok",
        stepSeconds: 60,
        intervalSeconds: 60,
        forSeconds: 300,
        cooldownSeconds: 0,
      },
      "u_1",
    );

    expect(requestJson.mock.calls[0][0]).toBe("PUT");
    // The id is encoded into the path rather than interpolated raw.
    expect(requestJson.mock.calls[0][1]).toBe(`${BASE}/alerts/watches/w%201`);
    const body = requestJson.mock.calls[0][2] as Record<string, unknown>;
    expect(body.on_no_data).toBe("ok");
    expect(body.interval_seconds).toBe(60);
    expect(body.actorId).toBe("u_1");
    // The id travels in the path, never in the body.
    expect(body.id).toBeUndefined();
  });

  it("drops the query parameters that were not asked for", async () => {
    ok({ items: [] });

    await alerts.listEvaluations({
      watchId: "w_1",
      notable: true,
      statuses: ["firing", "error"],
    });

    const url = requestJson.mock.calls[0][1] as string;
    expect(url).toContain("watchId=w_1");
    expect(url).toContain("notable=true");
    // A repeated parameter is repeated, not joined.
    expect(url).toContain("status=firing");
    expect(url).toContain("status=error");
    expect(url).not.toContain("incidentId");
    expect(url).not.toContain("before");
  });

  // `open` is only sent when it is true: sending open=false would be a filter
  // the service does not have, rather than the absence of one.
  it("omits a false flag rather than sending it", async () => {
    ok({ items: [] });
    await alerts.listIncidents({ open: false });
    expect(requestJson.mock.calls[0][1]).toBe(`${BASE}/alerts/incidents`);
  });

  it("carries the paging cursor through", async () => {
    ok({ items: [], next_before: "2026-09-06T10:00:00Z|e_1" });

    const res = await alerts.listEvaluations({ limit: 2 });
    expect(res.ok).toBe(true);
    if (!res.ok) return;
    expect(res.data.nextBefore).toBe("2026-09-06T10:00:00Z|e_1");
  });

  it("reports the last page with no cursor", async () => {
    ok({ items: [] });

    const res = await alerts.listEvaluations({});
    expect(res.ok).toBe(true);
    if (!res.ok) return;
    expect(res.data.nextBefore).toBeNull();
  });

  it("maps an evaluation and keeps its outcomes", async () => {
    ok({
      items: [
        {
          id: "e_1",
          watch_id: "w_1",
          incident_id: "i_1",
          evaluated_at: "2026-09-06T10:00:00Z",
          status: "firing",
          phase: "firing",
          previous_phase: "pending",
          transitioned: true,
          degraded: false,
          matched: 1,
          total: 2,
          window_from: "2026-09-06T09:45:00Z",
          window_to: "2026-09-06T10:00:00Z",
          reason: "condition_met",
          duration_ms: 12,
          outcomes: [
            {
              conditionId: "c_1",
              label: "error_rate gt",
              threshold: 0.05,
              observed: 0.41,
              verdict: "true",
              samples: 15,
              windowFrom: "",
              windowTo: "",
              kind: "threshold",
            },
          ],
        },
      ],
    });

    const res = await alerts.listEvaluations({});
    expect(res.ok).toBe(true);
    if (!res.ok) return;

    const [row] = res.data.items;
    expect(row.previousPhase).toBe("pending");
    expect(row.durationMs).toBe(12);
    // The outcome carries the threshold it was judged against, which is what
    // lets a three-week-old row still explain itself.
    expect(row.outcomes[0].threshold).toBe(0.05);
    expect(row.outcomes[0].observed).toBe(0.41);
  });

  it("passes an error result through untouched", async () => {
    requestJson.mockResolvedValue({
      ok: false,
      error: "minSamples 9 exceeds the 3-bucket window",
    });

    const res = await alerts.createWatch(
      {
        name: "x",
        description: "",
        enabled: true,
        severity: "warning",
        combinator: "all",
        conditions: [],
        actions: [],
        onNoData: "ok",
        stepSeconds: 60,
        intervalSeconds: 60,
        forSeconds: 0,
        cooldownSeconds: 0,
      },
      "u_1",
    );
    expect(res.ok).toBe(false);
    if (res.ok) return;
    // The service's own message names the field and the bound, so it must not
    // be replaced with something generic on the way through.
    expect(res.error).toContain("minSamples");
  });

  it("sends a mute and an un-mute", async () => {
    ok(undefined);
    await alerts.muteWatch("w_1", "2026-09-06T11:00:00Z");
    expect(requestJson.mock.calls[0][2]).toEqual({
      until: "2026-09-06T11:00:00Z",
    });

    requestJson.mockReset();
    ok(undefined);
    await alerts.muteWatch("w_1", null);
    expect(requestJson.mock.calls[0][2]).toEqual({ until: null });
  });
});
