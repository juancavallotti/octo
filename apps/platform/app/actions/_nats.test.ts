import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The NATS client, at the one place its rollup can quietly say something untrue.
 *
 * The broker reports at two levels and only one of them is per-subject: a
 * subscription's `msgs` is its own, while a connection's counters cover every
 * subject that client touches. The runtime's log shipper subscribes to several
 * subjects on a single connection, so attaching those connection counters to each
 * subject made two different destinations report byte-for-byte identical traffic —
 * which is what this pins.
 */

const requestJson = vi.fn();
vi.mock("@octo/http", () => ({
  requestJson: (method: string, url: string, body?: unknown) =>
    requestJson(method, url, body),
}));

import { getQueueStats } from "./_nats";

const BASE = "http://nats:8222";

const VARZ = {
  server_name: "NBJ",
  version: "2.10.29",
  now: "2026-08-14T00:00:00Z",
  uptime: "12d",
  connections: 1,
  total_connections: 36,
  in_msgs: 18843,
  out_msgs: 9119,
  in_bytes: 0,
  out_bytes: 0,
  slow_consumers: 0,
  subscriptions: 62,
};

/** One client, two subjects — the shape the identical-numbers bug showed up on. */
const CONNZ = {
  connections: [
    {
      cid: 26,
      name: "octo-logs",
      subscriptions: 2,
      pending_bytes: 0,
      in_msgs: 0,
      out_msgs: 7472,
      in_bytes: 0,
      out_bytes: 7_969_177,
      subscriptions_list_detail: [
        { subject: "internal.logs", msgs: 576 },
        { subject: "internal.traces", msgs: 6896 },
        // Reply inboxes are not destinations anyone consumes.
        { subject: "_INBOX.abc", msgs: 3 },
      ],
    },
  ],
};

function respond() {
  requestJson.mockImplementation((_m: string, url: string) =>
    Promise.resolve({ ok: true, data: url.includes("/varz") ? VARZ : CONNZ }),
  );
}

describe("the queue rollup", () => {
  beforeEach(() => {
    process.env.NATS_MONITOR_URL = BASE;
    respond();
  });

  afterEach(() => {
    vi.clearAllMocks();
    delete process.env.NATS_MONITOR_URL;
  });

  it("gives each destination its own message count, not the connection's", async () => {
    const res = await getQueueStats();
    if (!res.ok) throw new Error(res.error);

    const [logs, traces] = res.data.destinations;
    expect(logs.name).toBe("internal.logs");
    expect(traces.name).toBe("internal.traces");
    expect(logs.msgs).toBe(576);
    expect(traces.msgs).toBe(6896);

    // The subscriber row is per-subject too. This is the assertion that fails if
    // the connection's totals are ever flattened back into it.
    expect(logs.subscribers[0].msgs).toBe(576);
    expect(traces.subscribers[0].msgs).toBe(6896);
    expect(logs.subscribers[0].msgs).not.toBe(traces.subscribers[0].msgs);
  });

  it("keeps the connection's own totals whole, and marked as the connection's", async () => {
    const res = await getQueueStats();
    if (!res.ok) throw new Error(res.error);

    // Identical under both subjects — which is correct, and only legible because
    // it hangs off `connection` rather than off the destination.
    for (const dest of res.data.destinations) {
      expect(dest.subscribers[0].connection.outMsgs).toBe(7472);
      expect(dest.subscribers[0].connection.subscriptions).toBe(2);
    }
  });

  it("counts a connection once per destination, however many subs it holds", async () => {
    const res = await getQueueStats();
    if (!res.ok) throw new Error(res.error);

    for (const dest of res.data.destinations) {
      expect(dest.subscribers).toHaveLength(1);
      expect(dest.subscriptions).toBe(1);
      expect(dest.subscribers[0].cid).toBe(26);
    }
  });

  it("drops internal subjects, which are not queues", async () => {
    const res = await getQueueStats();
    if (!res.ok) throw new Error(res.error);

    expect(res.data.destinations.map((d) => d.subject)).toEqual([
      "internal.logs",
      "internal.traces",
    ]);
  });

  it("says so rather than guessing when monitoring is unconfigured", async () => {
    delete process.env.NATS_MONITOR_URL;
    const res = await getQueueStats();
    expect(res.ok).toBe(false);
  });
});
