// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The browser's end of a conversation with Dr. Octo, and the two fields it does
 * not own.
 *
 * The identity is written over whatever arrived, because the agent keys its memory
 * on the user id and accepting one from the browser would let anyone read anyone's
 * conversation by asking for it. The stop is turned into a header here rather than
 * passed through, because it ends a run — it is the one instruction in this route
 * that destroys work somebody is paying for.
 */

const auth = { currentWriteUserId: vi.fn() };
vi.mock("@/app/actions/_auth", () => auth);

// The guard is mocked for its module rather than its behaviour: importing it for
// real pulls in next-auth, which needs a request context this test has no use for.
vi.mock("@/app/auth/guard", () => ({
  AuthError: class AuthError extends Error {},
  ForbiddenError: class ForbiddenError extends Error {},
}));

vi.mock("@/app/actions/client/agentUrl", () => ({
  resolveAgentUrl: () => Promise.resolve({ ok: true, url: "http://dr-octo.local" }),
  forgetAgentUrl: () => {},
  orchestratorUrl: () => "http://orchestrator.local",
}));

const { POST } = await import("./route");

const fetchMock = vi.fn();

/** An upstream that streams nothing, which is all these cases read. */
function upstream(): Response {
  return {
    ok: true,
    status: 200,
    body: new ReadableStream({ start: (c) => c.close() }),
  } as unknown as Response;
}

function ask(body: unknown): Request {
  return new Request("http://platform.local/api/agent/chat", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/** The headers this route sent upstream. */
function sentHeaders(): Record<string, string> {
  return (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>;
}

describe("POST /api/agent/chat", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(upstream());
    auth.currentWriteUserId.mockResolvedValue({ id: "u-1", name: "Ada" });
  });

  it("carries a stop to the agent as the header its stopWhen reads", async () => {
    await POST(ask({ threadId: "t-1", message: "", stop: true }));

    expect(sentHeaders()["X-Agent-Stop"]).toBe("1");
  });

  it("sends no stop header for an ordinary message", async () => {
    await POST(ask({ threadId: "t-1", message: "how many integrations" }));

    expect(sentHeaders()["X-Agent-Stop"]).toBeUndefined();
  });

  // Strictly the boolean the panel sends. A truthy read would let the string
  // "false" — which is what a stop lost through a query string looks like — end a
  // run that nobody asked to end.
  it.each([["false"], [1], [{}], ["0"]])("does not stop on a truthy %s", async (stop) => {
    await POST(ask({ threadId: "t-1", message: "", stop }));

    expect(sentHeaders()["X-Agent-Stop"]).toBeUndefined();
  });

  // The agent keys its memory on the user id, so a forged block in the request
  // would be a way to read somebody else's conversation by asking for it.
  it("writes the signed-in identity over whatever the browser sent", async () => {
    await POST(ask({ threadId: "t-1", message: "hi", user: { id: "u-2", name: "Mallory" } }));

    const sent = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(sent.user).toEqual({ id: "u-1", name: "Ada" });
  });
});
