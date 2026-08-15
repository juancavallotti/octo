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
import type { QueueConnection } from "@/app/model/queues";

const BASE = "http://nats:8222";

const VARZ = {
  server_name: "NBJ",
  version: "2.10.29",
  now: "2026-08-14T00:00:00Z",
  uptime: "12d",
  connections: 1,
  total_connections: 36,
  in_msgs: 18843,
  out_msgs: 7472,
  in_bytes: 0,
  out_bytes: 0,
  slow_consumers: 0,
  subscriptions: 62,
};

/**
 * One consumer on two subjects — the shape the identical-numbers bug showed up
 * on — beside the publisher whose traffic it is consuming. The publisher is the
 * half that was never on screen, and the reason a consumer reading zero published
 * looked like it disagreed with the server's 18,843 in.
 */
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
    {
      cid: 27,
      name: "octo-runtime",
      subscriptions: 0,
      pending_bytes: 0,
      in_msgs: 18843,
      out_msgs: 0,
      in_bytes: 15_728_640,
      out_bytes: 0,
      subscriptions_list_detail: [],
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

  it("keeps connection totals off the destinations entirely", async () => {
    const res = await getQueueStats();
    if (!res.ok) throw new Error(res.error);

    // A subscriber carries what is true of this subject and nothing wider. The
    // fields that would repeat across subjects live on the connection instead.
    for (const dest of res.data.destinations) {
      expect(Object.keys(dest.subscribers[0]).sort()).toEqual([
        "cid",
        "msgs",
        "name",
        "queue",
        "subscriptions",
      ]);
    }
  });

  it("lists every open connection once, with its own totals", async () => {
    const res = await getQueueStats();
    if (!res.ok) throw new Error(res.error);

    expect(res.data.connections.map((c) => c.cid)).toEqual([26, 27]);
    // The publisher is here even though it consumes nothing, which is what makes
    // the headline counters add up rather than merely look wrong.
    const publisher = res.data.connections.find((c) => c.cid === 27)!;
    expect(publisher.inMsgs).toBe(18843);
  });

  // The comparison that read as a contradiction on screen: a consumer showing no
  // messages in against a server showing 18,843. Both are right — a connection's
  // counters are the broker's side, so a pure consumer publishes nothing — and it
  // only reads that way while the publisher is missing from the page. Pinned on
  // this fixture rather than as a law: a live server's /varz also counts system
  // account traffic that /connz does not list, so the two need not sum exactly.
  it("carries both sides of the traffic through untouched", async () => {
    const res = await getQueueStats();
    if (!res.ok) throw new Error(res.error);

    const connections = res.data.connections;
    const sum = (pick: (c: QueueConnection) => number) =>
      connections.reduce((total, c) => total + pick(c), 0);

    expect(sum((c) => c.inMsgs)).toBe(18843);
    expect(sum((c) => c.outMsgs)).toBe(7472);
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
