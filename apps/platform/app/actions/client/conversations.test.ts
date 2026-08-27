/**
 * The conversations client: where it reads a person's history from, and how it
 * scopes it.
 *
 * Both halves are load-bearing. The listing is scoped to the asker, because the
 * record is per person and a client that could choose the id could read anyone's
 * conversations. And the reads now go to the orchestrator rather than to the
 * agent's own pod, which is what makes history survive a reinstall.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const fetchAgentStatus = vi.fn();

vi.mock("./agentUrl", () => ({
  fetchAgentStatus: () => fetchAgentStatus(),
}));

import { listConversations, readConversation, DR_OCTO_AGENT_ID } from "./conversations";

const ada = { id: "u-1", name: "Ada" };

// The signature is declared rather than inferred: the assertions below read
// `mock.calls[0]`, and a mock inferred from a zero-argument implementation types
// that as the empty tuple.
function respondWith(status: number, body: unknown) {
  return vi.fn<(url: string, init: RequestInit) => Promise<Response>>(
    async () => new Response(JSON.stringify(body), { status }),
  );
}

beforeEach(() => {
  process.env.ORCHESTRATOR_URL = "http://orchestrator";
  fetchAgentStatus.mockResolvedValue({ state: "deployed", integrationId: "int-1" });
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchAgentStatus.mockReset();
});

describe("listConversations", () => {
  it("reads the orchestrator's threads for this agent and this person", async () => {
    const fetchMock = respondWith(200, {
      threads: [
        {
          agentId: DR_OCTO_AGENT_ID,
          threadKey: "t-1",
          title: "Deploying",
          version: 2,
          turnCount: 4,
          createdAt: "2026-08-01T00:00:00Z",
          lastActivityAt: "2026-08-02T00:00:00Z",
        },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await listConversations(ada);

    expect(result).toEqual({
      ok: true,
      data: [{ id: "t-1", title: "Deploying", updatedAt: "2026-08-02T00:00:00Z" }],
    });
    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe(
      `http://orchestrator/integrations/int-1/agent-memory/${DR_OCTO_AGENT_ID}/threads?userId=u-1`,
    );
  });

  // A brand new person has no conversations, and a panel that read a list off
  // nothing would break on somebody's first visit.
  it("reads an empty listing as an empty list", async () => {
    vi.stubGlobal("fetch", respondWith(200, { threads: [] }));
    await expect(listConversations(ada)).resolves.toEqual({ ok: true, data: [] });
  });

  // The conversation record is keyed on the integration, so without an installed
  // agent there is nothing to address — and saying so beats a request to a URL
  // with an empty segment in it.
  it("reports an uninstalled agent rather than guessing an integration", async () => {
    fetchAgentStatus.mockResolvedValue({ state: "not_installed" });
    const result = await listConversations(ada);
    expect(result.ok).toBe(false);
  });

  // A title is what a list shows. A conversation the runtime opened but nothing
  // named still has to be pickable.
  it("falls back to the thread key when nothing has named the conversation", async () => {
    vi.stubGlobal(
      "fetch",
      respondWith(200, {
        threads: [{ threadKey: "t-9", title: "", lastActivityAt: "2026-08-02T00:00:00Z" }],
      }),
    );
    const result = await listConversations(ada);
    expect(result).toEqual({
      ok: true,
      data: [{ id: "t-9", title: "t-9", updatedAt: "2026-08-02T00:00:00Z" }],
    });
  });
});

describe("readConversation", () => {
  it("maps stored turns onto the two roles the panel renders", async () => {
    const fetchMock = respondWith(200, {
      thread: { threadKey: "t-1", title: "Deploying", userId: "u-1" },
      turns: [
        { seq: 1, role: "user", text: "how do I deploy?", createdAt: "2026-08-01T00:00:00Z" },
        { seq: 2, role: "assistant", text: "roll it out", createdAt: "2026-08-01T00:00:01Z" },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await readConversation(ada, "t-1");

    expect(result).toEqual({
      ok: true,
      data: {
        threadId: "t-1",
        title: "Deploying",
        turns: [
          { role: "user", text: "how do I deploy?" },
          { role: "agent", text: "roll it out" },
        ],
      },
    });
    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe(
      `http://orchestrator/integrations/int-1/agent-memory/${DR_OCTO_AGENT_ID}/threads/t-1`,
    );
  });

  // The route is addressed by thread, so the scoping the listing does by query
  // has to be done again here — otherwise a thread key is enough to read somebody
  // else's conversation.
  it("reads another person's conversation as an empty one", async () => {
    vi.stubGlobal(
      "fetch",
      respondWith(200, {
        thread: { threadKey: "t-1", title: "Theirs", userId: "u-2" },
        turns: [{ seq: 1, role: "user", text: "private", createdAt: "2026-08-01T00:00:00Z" }],
      }),
    );
    const result = await readConversation(ada, "t-1");
    expect(result).toEqual({ ok: true, data: { threadId: "t-1", title: "", turns: [] } });
  });

  it("escapes a thread key that would otherwise span path segments", async () => {
    const fetchMock = respondWith(200, { thread: { threadKey: "a/b" }, turns: [] });
    vi.stubGlobal("fetch", fetchMock);

    await readConversation(ada, "a/b");

    const [url] = fetchMock.mock.calls[0];
    expect(url).toContain("/threads/a%2Fb");
  });
});
