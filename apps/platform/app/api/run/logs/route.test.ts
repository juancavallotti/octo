// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The one part of RUN that still needs a route, because a server action cannot stream.
 * It is also the one part that cannot state who it is: an EventSource sets no headers, so
 * the integration rides in the query string and the *user* comes from the session. Pairing
 * a server-resolved user with untrusted client input is what keeps one caller's stream out
 * of another's run, and it is exactly the kind of plumbing that breaks quietly — the stream
 * still opens, it just follows the wrong thing.
 */

const runner = { status: vi.fn(), logs: vi.fn() };
vi.mock("@/app/run/remoteRunner", () => ({ remoteRunner: runner }));

const auth = { currentUserId: vi.fn() };
vi.mock("@/app/actions/_auth", () => auth);

const { GET } = await import("./route");

const COOKIE = "aaaa0000";
const TAB = "3f9c1e7b8a4d2c5e6f0b1a9d8c7e6f5a";

function request(query: string, headers: Record<string, string> = {}): Request {
  return new Request(`http://editor.local/api/run/logs${query}`, {
    headers: { cookie: `octo_ns=${COOKIE}`, ...headers },
  });
}

/** Read the whole SSE body. Every stub below ends its stream, so this terminates. */
async function readAll(res: Response): Promise<string> {
  const reader = res.body!.getReader();
  const decoder = new TextDecoder();
  let out = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    out += decoder.decode(value, { stream: true });
  }
  return out;
}

/** A log stream that yields `lines` and ends. */
function yielding(lines: { seq: number; text: string }[]) {
  return async function* () {
    for (const line of lines) yield line;
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  auth.currentUserId.mockResolvedValue("user-7");
  runner.status.mockResolvedValue({
    running: true,
    exposable: false,
    port: null,
    testUrl: null,
    reloadsOnSave: true,
  });
  runner.logs.mockImplementation(yielding([{ seq: 7, text: "listening on 8080" }]));
});

describe("GET /api/run/logs", () => {
  it("streams the run's lines as SSE events carrying their sequence", async () => {
    const body = await readAll(await GET(request(`?tab=${TAB}&integrationId=int-1`)));

    expect(body).toContain("id: 7\ndata: listening on 8080\n\n");
  });

  // The user is never taken from the request. An integration id is guessable; a run
  // addressed by somebody else's simply is not found.
  it("addresses the run by the session's user and the query's integration", async () => {
    await readAll(await GET(request(`?tab=${TAB}&integrationId=int-1`)));

    expect(runner.logs).toHaveBeenCalledWith(
      expect.objectContaining({ userId: "user-7", integrationId: "int-1" }),
      expect.anything(),
    );
  });

  // Not an empty stream: an EventSource retries a stream that merely closes, every few
  // seconds, forever — while it treats a non-200 as final. So "nothing is running" has to
  // be a status code.
  it("answers 404 when no run is there to follow", async () => {
    runner.status.mockResolvedValue({ running: false });

    const res = await GET(request(`?tab=${TAB}&integrationId=int-1`));

    expect(res.status).toBe(404);
    expect(runner.logs).not.toHaveBeenCalled();
  });

  // The client's de-duplication drops anything at or below the last sequence it saw, so a
  // resumed stream that started numbering again would have its fresh lines discarded.
  it("resumes after the sequence the client last received", async () => {
    await readAll(
      await GET(request(`?tab=${TAB}&integrationId=int-1`, { "Last-Event-ID": "41" })),
    );

    expect(runner.logs).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ fromSeq: 41 }),
    );
  });

  it("ignores a Last-Event-ID that is not a sequence number", async () => {
    await readAll(
      await GET(request(`?tab=${TAB}&integrationId=int-1`, { "Last-Event-ID": "" })),
    );

    expect(runner.logs).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ fromSeq: undefined }),
    );
  });

  it("refuses a caller whose user cannot be resolved", async () => {
    auth.currentUserId.mockRejectedValue(new Error("unauthenticated"));

    const res = await GET(request(`?tab=${TAB}&integrationId=int-1`));

    expect(res.status).toBe(401);
    expect(runner.status).not.toHaveBeenCalled();
  });

  // A failure to reach the orchestrator is upstream's, not the caller's — and it must not
  // read as "nothing is running", which would stop the client retrying.
  it("reports a failed lookup as a bad gateway", async () => {
    runner.status.mockRejectedValue(new Error("orchestrator not configured"));

    expect((await GET(request(`?tab=${TAB}&integrationId=int-1`))).status).toBe(502);
  });

  // The stream is often a session's first request, and the run cookie backs the one-shot
  // debug operations — so this route still mints it even though a dev run is not keyed on it.
  it("mints a namespace cookie for a browser that has none", async () => {
    const res = await GET(new Request("http://editor.local/api/run/logs?integrationId=int-1"));
    await res.body!.cancel();

    expect(res.headers.get("Set-Cookie")).toMatch(/^octo_ns=[a-z0-9]{8}; .*HttpOnly/);
  });

  // A draft has no integration to name, so there is nothing to follow — and the runner is
  // asked rather than second-guessed here, so the answer comes from one place.
  it("answers 404 for a caller that names no integration", async () => {
    runner.status.mockResolvedValue({ running: false });

    expect((await GET(request(`?tab=${TAB}`))).status).toBe(404);
    expect(runner.status).toHaveBeenCalledWith(
      expect.objectContaining({ integrationId: undefined }),
    );
  });
});
