/**
 * The conversations client: what it sends, and what it does when the agent is not
 * there. Both halves are load-bearing — the identity it sends is the only thing
 * scoping the record, and a stale address is the ordinary failure after a redeploy.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const resolveAgentUrl = vi.fn();
const forgetAgentUrl = vi.fn();

vi.mock("./agentUrl", () => ({
  resolveAgentUrl: () => resolveAgentUrl(),
  forgetAgentUrl: () => forgetAgentUrl(),
}));

import { listConversations, readConversation } from "./conversations";

const ada = { id: "u-1", name: "Ada" };

function respondWith(status: number, body: unknown) {
  return vi.fn(async () => new Response(JSON.stringify(body), { status }));
}

beforeEach(() => {
  resolveAgentUrl.mockResolvedValue({ ok: true, url: "http://agent" });
  forgetAgentUrl.mockClear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("listConversations", () => {
  it("sends the asker and nothing else, because the record is keyed on them", async () => {
    const fetchMock = respondWith(200, { items: [{ id: "t-1", title: "Deploying", updatedAt: "2026-08-01T00:00:00Z" }] });
    vi.stubGlobal("fetch", fetchMock);

    const result = await listConversations(ada);

    expect(result).toEqual({
      ok: true,
      data: [{ id: "t-1", title: "Deploying", updatedAt: "2026-08-01T00:00:00Z" }],
    });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("http://agent/conversations");
    expect(JSON.parse(init.body as string)).toEqual({ user: ada });
  });

  // A brand new user has no index object at all, and a panel that read `.items`
  // off nothing would break on somebody's first visit.
  it("reads a missing list as an empty one", async () => {
    vi.stubGlobal("fetch", respondWith(200, {}));
    await expect(listConversations(ada)).resolves.toEqual({ ok: true, data: [] });
  });

  it("reports the agent not being deployed rather than throwing", async () => {
    resolveAgentUrl.mockResolvedValue({ ok: false, error: "not deployed", status: 503 });
    const result = await listConversations(ada);
    expect(result).toEqual({ ok: false, error: "not deployed" });
  });

  // The address is cached, so a pod replaced by a redeploy answers nothing for the
  // whole TTL unless the failure drops it.
  it("forgets a cached address that did not answer", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("connect ECONNREFUSED"); }));
    const result = await listConversations(ada);
    expect(result.ok).toBe(false);
    expect(forgetAgentUrl).toHaveBeenCalled();
  });
});

describe("readConversation", () => {
  it("addresses the thread in the path and the person in the body", async () => {
    const fetchMock = respondWith(200, { threadId: "t 1", title: "Deploying", turns: [] });
    vi.stubGlobal("fetch", fetchMock);

    await readConversation(ada, "t 1");

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    // Encoded, because a thread id is whatever the browser generated and lands in
    // a path segment.
    expect(url).toBe("http://agent/conversations/t%201");
    expect(JSON.parse(init.body as string)).toEqual({ user: ada });
  });
});
