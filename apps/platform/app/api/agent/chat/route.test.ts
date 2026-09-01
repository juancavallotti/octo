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

  it("carries an authorization to the agent as the headers it reads", async () => {
    await POST(ask({ threadId: "t-1", message: "", authorize: { id: "auth_1", allow: true } }));

    expect(sentHeaders()["X-Agent-Auth-Id"]).toBe("auth_1");
    expect(sentHeaders()["X-Agent-Auth"]).toBe("allow");
  });

  it("carries a denial as a denial", async () => {
    await POST(ask({ threadId: "t-1", message: "", authorize: { id: "auth_1", allow: false } }));

    expect(sentHeaders()["X-Agent-Auth"]).toBe("deny");
  });

  // Allowing is the one instruction here that lets something happen rather than
  // stopping it, so it is granted by the boolean and nothing else. Everything a
  // value could have turned into on the way — a string, a number, a lost field —
  // denies.
  it.each([["true"], [1], [{}], [null], [undefined]])(
    "denies rather than allowing on a truthy %s",
    async (allow) => {
      await POST(ask({ threadId: "t-1", message: "", authorize: { id: "auth_1", allow } }));

      expect(sentHeaders()["X-Agent-Auth"]).toBe("deny");
    },
  );

  // An answer says yes or no to a particular call. Without an id there is nothing
  // for it to be about, so it is not sent at all rather than sent to be matched
  // against whatever the run happens to be holding.
  it.each([[{ allow: true }], [{ id: "", allow: true }], [{ id: 7, allow: true }], ["auth_1"], [[]]])(
    "sends nothing for an answer with no id: %s",
    async (authorize) => {
      await POST(ask({ threadId: "t-1", message: "", authorize }));

      expect(sentHeaders()["X-Agent-Auth-Id"]).toBeUndefined();
      expect(sentHeaders()["X-Agent-Auth"]).toBeUndefined();
    },
  );

  it("sends no authorization headers for an ordinary message", async () => {
    await POST(ask({ threadId: "t-1", message: "how many integrations" }));

    expect(sentHeaders()["X-Agent-Auth-Id"]).toBeUndefined();
  });

  // `null` is valid JSON and is not an object, so reading a field off it throws
  // and a plainly bad request comes back as a 500. Arrays and scalars parse too
  // and carry none of the fields this route reads.
  it.each([["null"], ["[]"], ['"text"'], ["7"], ["not json at all"]])(
    "refuses a body that is not an object: %s",
    async (raw) => {
      const res = await POST(
        new Request("http://platform.local/api/agent/chat", { method: "POST", body: raw }),
      );

      expect(res.status).toBe(400);
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );

  // The agent keys its memory on the user id, so a forged block in the request
  // would be a way to read somebody else's conversation by asking for it.
  it("writes the signed-in identity over whatever the browser sent", async () => {
    await POST(ask({ threadId: "t-1", message: "hi", user: { id: "u-2", name: "Mallory" } }));

    const sent = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(sent.user).toEqual({ id: "u-1", name: "Ada" });
  });
});
