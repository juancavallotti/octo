import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The remote app runner: the platform's answer to "the editor's Run cannot live in a BFF
 * replica's memory". Everything here is about holding no state — every operation re-derives
 * the run from (user, integration) by asking the orchestrator — so what is worth testing is
 * that it never needs to remember anything, and that the two places where a dev run is
 * genuinely unlike a local child process are handled deliberately:
 *
 *   - a run's existence IS its status, so an empty list is a complete answer;
 *   - a pod log has no cursor, so a resumed stream numbers onwards from where the client
 *     got to rather than starting again.
 */

const client = {
  listDevRuns: vi.fn(),
  ensureDevRun: vi.fn(),
  reloadDevRun: vi.fn(),
  deleteDevRun: vi.fn(),
  openDevRunLogs: vi.fn(),
};
vi.mock("@/app/actions/_client", () => client);

const { logTail, remoteRunner } = await import("./remoteRunner");

const KEY = { namespace: "ns-abc123", userId: "user-7", integrationId: "int-1" };

/** A dev run as the orchestrator reports it. */
function devRun(over: Record<string, unknown> = {}) {
  return {
    id: "3f7c2b90-0000-4000-8000-000000000000",
    userId: "user-7",
    integrationId: "int-1",
    status: "running",
    ready: true,
    host: "abc12def.apps.example.com",
    testUrl: "https://abc12def.apps.example.com",
    ...over,
  };
}

/** A byte stream carrying `chunks` verbatim, so a test can split them where it likes. */
function textStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const c of chunks) controller.enqueue(encoder.encode(c));
      controller.close();
    },
  });
}

/** Drain a log stream into `${seq}:${text}` lines. */
async function drain(opts: { fromSeq?: number } = {}): Promise<string[]> {
  const out: string[] = [];
  const signal = new AbortController().signal;
  for await (const line of remoteRunner.logs(KEY, { ...opts, signal })) {
    out.push(`${line.seq}:${line.text}`);
  }
  return out;
}

beforeEach(() => {
  vi.clearAllMocks();
  client.listDevRuns.mockResolvedValue({ ok: true, data: [devRun()] });
  client.ensureDevRun.mockResolvedValue({ ok: true, data: { ...devRun(), created: true } });
  client.reloadDevRun.mockResolvedValue({ ok: true, data: undefined });
  client.deleteDevRun.mockResolvedValue({ ok: true, data: undefined });
});

describe("status", () => {
  it("reads the run back from the cluster, by owner and integration", async () => {
    expect(await remoteRunner.status(KEY)).toEqual({
      running: true,
      exposable: true,
      port: null,
      testUrl: "https://abc12def.apps.example.com",
      reloadsOnSave: true,
    });
    expect(client.listDevRuns).toHaveBeenCalledWith("user-7", "int-1");
  });

  // Nothing running is the ordinary answer on every editor mount, and there is no stored
  // row that could have said otherwise — so it is a state, not an error.
  it("reports no run when the user has none for this integration", async () => {
    client.listDevRuns.mockResolvedValue({ ok: true, data: [] });

    expect(await remoteRunner.status(KEY)).toEqual({
      running: false,
      exposable: false,
      port: null,
      testUrl: null,
      reloadsOnSave: true,
    });
  });

  // A draft cannot name a run, and asking anyway would send the orchestrator a query it
  // has no answer for.
  it("asks nothing for a key that cannot name a run", async () => {
    expect(await remoteRunner.status({ namespace: "ns-abc123" })).toEqual(
      expect.objectContaining({ running: false }),
    );
    expect(client.listDevRuns).not.toHaveBeenCalled();
  });

  // The pod exists, so the run exists — and its logs are exactly what a developer wants
  // while it comes up, or fails to. The reason is what says it is not usable yet.
  it("counts a pod that is still starting as a run, with the reason why", async () => {
    client.listDevRuns.mockResolvedValue({
      ok: true,
      data: [devRun({ status: "pending", ready: false })],
    });

    expect(await remoteRunner.status(KEY)).toEqual(
      expect.objectContaining({ running: true, reason: "the dev run is starting" }),
    );
  });

  it("passes the cluster's own reason through when it has one", async () => {
    client.listDevRuns.mockResolvedValue({
      ok: true,
      data: [devRun({ status: "failed", ready: false, reason: "ImagePullBackOff" })],
    });

    expect(await remoteRunner.status(KEY)).toEqual(
      expect.objectContaining({ reason: "ImagePullBackOff" }),
    );
  });

  // The endpoint is published as soon as the run is created, so offering the URL before the
  // pod answers would hand the user a link that 502s.
  it("withholds the address until the run is ready, but still calls it networked", async () => {
    client.listDevRuns.mockResolvedValue({
      ok: true,
      data: [devRun({ ready: false })],
    });

    expect(await remoteRunner.status(KEY)).toEqual(
      expect.objectContaining({ exposable: true, testUrl: null }),
    );
  });

  it("reports a non-networked run as unexposable", async () => {
    client.listDevRuns.mockResolvedValue({
      ok: true,
      data: [devRun({ host: undefined, testUrl: undefined })],
    });

    expect(await remoteRunner.status(KEY)).toEqual(
      expect.objectContaining({ exposable: false, testUrl: null }),
    );
  });
});

describe("start", () => {
  // A fresh pod's sidecar pulls the definition as it comes up, so a reload would be a
  // second pull of the same thing.
  it("creates the run and leaves the first pull to the sidecar", async () => {
    expect(await remoteRunner.start(KEY, { yaml: "flows: []\n" })).toEqual(
      expect.objectContaining({ running: true }),
    );
    expect(client.ensureDevRun).toHaveBeenCalledWith("user-7", "int-1");
    expect(client.reloadDevRun).not.toHaveBeenCalled();
  });

  // Attaching is what a second tab — or the same user coming back — does. Reloading then
  // makes Run mean "run what is saved now", which matters because the reload a save fires
  // is best-effort: a save must never fail because a pod was unreachable.
  it("reloads when it attaches to a run that was already there", async () => {
    client.ensureDevRun.mockResolvedValue({
      ok: true,
      data: { ...devRun(), created: false },
    });

    await remoteRunner.start(KEY, { yaml: "flows: []\n" });

    expect(client.reloadDevRun).toHaveBeenCalledWith("user-7", devRun().id);
  });

  // The run is up either way; refusing the Run because the reload did not land would turn
  // a stale generation into no run at all.
  it("still reports the attached run when its reload does not land", async () => {
    client.ensureDevRun.mockResolvedValue({
      ok: true,
      data: { ...devRun(), created: false },
    });
    client.reloadDevRun.mockResolvedValue({ ok: false, error: "sidecar not answering" });

    expect(await remoteRunner.start(KEY, { yaml: "flows: []\n" })).toEqual(
      expect.objectContaining({ running: true }),
    );
  });

  it("refuses a draft", async () => {
    await expect(
      remoteRunner.start({ namespace: "ns-abc123" }, { yaml: "flows: []\n" }),
    ).rejects.toThrow(/Save this integration/);
    expect(client.ensureDevRun).not.toHaveBeenCalled();
  });

  it("raises what the orchestrator refused the run for", async () => {
    client.ensureDevRun.mockResolvedValue({ ok: false, error: "dev runs are not available" });

    await expect(remoteRunner.start(KEY, { yaml: "" })).rejects.toThrow(
      "dev runs are not available",
    );
  });
});

describe("stop", () => {
  // The id is looked up rather than remembered: it is derived from an HMAC this side has
  // no key for, so there is nothing to cache even if caching were safe across replicas.
  it("looks the run up and deletes it", async () => {
    expect(await remoteRunner.stop(KEY)).toEqual(
      expect.objectContaining({ running: false, testUrl: null }),
    );
    expect(client.deleteDevRun).toHaveBeenCalledWith("user-7", devRun().id);
  });

  it("is a no-op when nothing is running", async () => {
    client.listDevRuns.mockResolvedValue({ ok: true, data: [] });

    expect(await remoteRunner.stop(KEY)).toEqual(expect.objectContaining({ running: false }));
    expect(client.deleteDevRun).not.toHaveBeenCalled();
  });
});

describe("sync", () => {
  it("asks the run to pick up what was saved", async () => {
    await remoteRunner.sync(KEY, { yaml: "flows: []\n" });

    expect(client.reloadDevRun).toHaveBeenCalledWith("user-7", devRun().id);
  });

  // Mirrors the local runner, whose sync is a no-op when nothing is running.
  it("does nothing when there is no run to reload", async () => {
    client.listDevRuns.mockResolvedValue({ ok: true, data: [] });

    expect(await remoteRunner.sync(KEY, { yaml: "" })).toEqual(
      expect.objectContaining({ running: false }),
    );
    expect(client.reloadDevRun).not.toHaveBeenCalled();
  });
});

describe("logs", () => {
  it("splits the run's output into numbered lines", async () => {
    client.openDevRunLogs.mockResolvedValue({
      ok: true,
      data: textStream(["starting\nlistening on 8080\n"]),
    });

    expect(await drain()).toEqual(["0:starting", "1:listening on 8080"]);
    expect(client.openDevRunLogs).toHaveBeenCalledWith(
      "user-7",
      devRun().id,
      expect.objectContaining({ tail: 500, follow: true }),
    );
  });

  // A line does not arrive whole: the orchestrator forwards whatever Kubernetes hands it,
  // so a newline can land in the next chunk — or a multi-byte character can straddle two.
  it("joins a line split across chunks", async () => {
    client.openDevRunLogs.mockResolvedValue({
      ok: true,
      data: textStream(["reloa", "ding — ", "config changed\nready\n"]),
    });

    expect(await drain()).toEqual(["0:reloading — config changed", "1:ready"]);
  });

  // The container was killed mid-write. That last line is often the interesting one.
  it("keeps a final line that has no newline after it", async () => {
    client.openDevRunLogs.mockResolvedValue({
      ok: true,
      data: textStream(["panic: nil map"]),
    });

    expect(await drain()).toEqual(["0:panic: nil map"]);
  });

  // Numbering onwards from what the client last saw, not from zero. Its de-duplication
  // drops anything at or below that sequence, so restarting the count would make it
  // discard the fresh lines instead of the replayed ones.
  it("numbers a resumed stream after the sequence the client already had", async () => {
    client.openDevRunLogs.mockResolvedValue({
      ok: true,
      data: textStream(["one\ntwo\n"]),
    });

    expect(await drain({ fromSeq: 41 })).toEqual(["42:one", "43:two"]);
  });

  it("yields nothing when there is no run to follow", async () => {
    client.listDevRuns.mockResolvedValue({ ok: true, data: [] });

    expect(await drain()).toEqual([]);
    expect(client.openDevRunLogs).not.toHaveBeenCalled();
  });

  // Raised rather than swallowed: a run whose pod is not scheduled yet has no logs, and the
  // caller's client should retry rather than sit on a stream that will never produce a line.
  it("raises when the stream could not be opened", async () => {
    client.openDevRunLogs.mockResolvedValue({
      ok: false,
      error: "the dev run has no pod to read yet",
    });

    await expect(drain()).rejects.toThrow("the dev run has no pod to read yet");
  });
});

describe("logTail", () => {
  // The counterpart for a caller that reads logs rather than watching them — an agent's
  // get_run_logs. It must NOT follow: a stream with no end is not a document, and whoever
  // asked would wait on it forever.
  it("reads a bounded document rather than following", async () => {
    client.openDevRunLogs.mockResolvedValue({
      ok: true,
      data: textStream(["starting\nready\n"]),
    });

    expect(await logTail(KEY)).toEqual([
      { seq: 0, text: "starting" },
      { seq: 1, text: "ready" },
    ]);
    expect(client.openDevRunLogs).toHaveBeenCalledWith(
      "user-7",
      devRun().id,
      expect.objectContaining({ follow: false }),
    );
  });

  // "No run" and "a run that has not spoken yet" are the same answer to a reader: there is
  // nothing to read. Distinguishing them would make the caller handle a case it cannot act on.
  it("reads nothing when there is no run", async () => {
    client.listDevRuns.mockResolvedValue({ ok: true, data: [] });

    expect(await logTail(KEY)).toEqual([]);
    expect(client.openDevRunLogs).not.toHaveBeenCalled();
  });

  it("raises what the orchestrator refused the read for", async () => {
    client.openDevRunLogs.mockResolvedValue({ ok: false, error: "dev run not found" });

    await expect(logTail(KEY)).rejects.toThrow("dev run not found");
  });
});
