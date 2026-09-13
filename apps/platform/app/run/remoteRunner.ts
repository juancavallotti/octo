import type {
  AppRunner,
  LogLine,
  LogStreamOptions,
  RunKey,
  RunState,
} from "@octo/run-host";
import * as client from "@/app/actions/_client";
import type { DevRun } from "@/app/model/devruns";

/**
 * The platform's {@link AppRunner}: the running app lives in a dev-run pod the
 * orchestrator owns, not in this process.
 *
 * Nothing here holds state. Every operation derives what it needs from
 * (user, integration) by asking the orchestrator, so any replica can serve any
 * request. Two consequences:
 *
 *   - **The run has no id here.** A dev run's id is derived from
 *     (user, integration) by an HMAC the orchestrator keys, so this side cannot
 *     compute it and must not cache it; operations that need one look the run up.
 *   - **Nothing here pushes config.** The run's sidecar pulls the *saved*
 *     definition, so `yaml` is ignored throughout and a draft cannot be run at
 *     all. That is what {@link RunState.reloadsOnSave} announces.
 */

/** How much log history the orchestrator replays before it starts tailing. */
const LOG_TAIL = 500;

/**
 * How long a non-following log read may take. A follow needs no bound — it ends when the
 * caller aborts — but a read that is supposed to end on its own and does not would
 * otherwise hang whoever asked for it.
 */
const LOG_READ_TIMEOUT_MS = 10_000;

/** What a caller is told when the editor asks to run something it has never saved. */
export const UNSAVED =
  "Save this integration before running it — the running app reads the saved definition.";

/**
 * The state of no run. Reported rather than raised: nothing running is an ordinary
 * answer. `reloadsOnSave` is true even here, since it describes the backend rather
 * than any one run.
 */
const NOT_RUNNING: RunState = {
  running: false,
  exposable: false,
  port: null,
  testUrl: null,
  reloadsOnSave: true,
};

/**
 * The run's address, or null when this key cannot name one. A dev run is
 * (user, integration) and nothing else; the key's namespace is unused, because a
 * dev pod is not shared and has nothing to separate.
 */
function addressOf(key: RunKey): { userId: string; integrationId: string } | null {
  if (!key.userId || !key.integrationId) return null;
  return { userId: key.userId, integrationId: key.integrationId };
}

/** The dev run for this key, or null when nothing is running for it. */
async function current(key: RunKey): Promise<DevRun | null> {
  const at = addressOf(key);
  if (!at) return null;
  const res = await client.listDevRuns(at.userId, at.integrationId);
  if (!res.ok) throw new Error(res.error);
  // At most one: the workload's name is derived from the same pair, so the cluster
  // itself enforces that there is a single run per (user, integration).
  return res.data[0] ?? null;
}

/**
 * Why the run is not usable yet, when the cluster can say — an image it cannot pull, a
 * pod nothing has scheduled, a crash loop. Absent once the run is ready, because a
 * healthy run needs no explanation.
 */
function reasonOf(run: DevRun): string | undefined {
  if (run.ready) return undefined;
  if (run.reason) return run.reason;
  if (run.status === "failed") return "the dev run's pod failed to start";
  return "the dev run is starting";
}

function stateOf(run: DevRun): RunState {
  const reason = reasonOf(run);
  return {
    // A dev run that exists is a run, whatever phase its pod is in: stopping deletes
    // the workload, so a stopped run is absent from the list rather than listed.
    running: true,
    // The orchestrator publishes an endpoint exactly when the definition declares an
    // HTTP_PORT, so having a host to advertise IS being networked.
    exposable: !!run.testUrl,
    // Always null: a dev pod owns its network namespace and listens on a constant,
    // so there is no allocated port to record.
    port: null,
    // Withheld until ready: the endpoint is published as soon as the run is created,
    // so offering the URL earlier hands out a link that answers 502.
    testUrl: run.ready ? (run.testUrl ?? null) : null,
    reloadsOnSave: true,
    ...(reason !== undefined ? { reason } : {}),
  };
}

/**
 * Split a byte stream into lines.
 *
 * Text-decoded incrementally: a chunk boundary can land inside a multi-byte character,
 * and `{ stream: true }` is what keeps that from becoming a replacement character in the
 * middle of a log line.
 */
async function* lines(
  body: ReadableStream<Uint8Array>,
  signal: AbortSignal,
): AsyncGenerator<string> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const cancel = () => void reader.cancel().catch(() => {});
  // An abort has to reach the socket, not just end the loop, or the reader stays
  // parked on a follow and holds the connection open behind it.
  signal.addEventListener("abort", cancel, { once: true });

  let buffered = "";
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffered += decoder.decode(value, { stream: true });
      let nl = buffered.indexOf("\n");
      while (nl !== -1) {
        yield buffered.slice(0, nl);
        buffered = buffered.slice(nl + 1);
        nl = buffered.indexOf("\n");
      }
    }
    // A final line with no newline after it is still a line — the container was
    // killed mid-write, and it is often the interesting one.
    buffered += decoder.decode();
    if (buffered !== "") yield buffered;
  } finally {
    signal.removeEventListener("abort", cancel);
    // Cancel rather than release: releasing a reader with a read still outstanding
    // throws, and a generator closed early always has one.
    cancel();
  }
}

/**
 * Follow the run's logs, numbering each line for de-duplication.
 *
 * Numbering continues after `fromSeq` rather than restarting at zero, so a
 * reconnect does not hand back sequences a reader has already dropped. A pod log
 * has no cursor to resume from, only "the last N lines", so a reconnect repeats
 * the replayed tail rather than risking a lost line.
 */
async function* followLogs(
  key: RunKey,
  opts: LogStreamOptions,
): AsyncGenerator<LogLine> {
  const run = await current(key);
  if (!run || opts.signal.aborted) return;

  const res = await client.openDevRunLogs(run.userId, run.id, {
    tail: LOG_TAIL,
    follow: true,
    signal: opts.signal,
  });
  if (!res.ok) {
    // An abort is how this stream normally ends, and surfaces as a failed request.
    if (opts.signal.aborted) return;
    // A run whose pod is not scheduled yet has no logs, and says so rather than
    // pretending to an empty stream. Raised so the stream ends and can be retried.
    throw new Error(res.error);
  }

  let seq = (opts.fromSeq ?? -1) + 1;
  for await (const text of lines(res.data, opts.signal)) {
    yield { seq: seq++, text };
  }
}

export const remoteRunner: AppRunner = {
  async status(key) {
    const run = await current(key);
    return run ? stateOf(run) : NOT_RUNNING;
  },

  /**
   * Start the run, or attach to the one already running this integration.
   *
   * An attach reloads, so starting always means "run what is saved now" even if a
   * save's own notification never landed; a fresh pod needs no reload, since its
   * sidecar's first pull is the load. The reload is best-effort: the run is up
   * either way, and a stale generation beats no run.
   */
  async start(key) {
    const at = addressOf(key);
    if (!at) throw new Error(UNSAVED);
    const res = await client.ensureDevRun(at.userId, at.integrationId);
    if (!res.ok) throw new Error(res.error);
    if (!res.data.created) {
      await client.reloadDevRun(at.userId, res.data.id);
    }
    return stateOf(res.data);
  },

  async stop(key) {
    const run = await current(key);
    if (!run) return NOT_RUNNING;
    const res = await client.deleteDevRun(run.userId, run.id);
    if (!res.ok) throw new Error(res.error);
    return NOT_RUNNING;
  },

  /**
   * Ask the run to pick up the saved definition.
   *
   * The state returned describes the run as it was *before* the reload: the sidecar
   * pulls and the runtime re-reads its directory after this returns, so the state
   * after it comes from a later status call.
   */
  async sync(key) {
    const run = await current(key);
    if (!run) return NOT_RUNNING;
    const res = await client.reloadDevRun(run.userId, run.id);
    if (!res.ok) throw new Error(res.error);
    return stateOf(run);
  },

  logs: (key, opts) => followLogs(key, opts),
};

/**
 * The run's recent output as a finished document, oldest line first — the
 * counterpart to {@link AppRunner.logs} for a caller that reads logs rather than
 * watches them.
 *
 * An empty result covers both no run and a run that has not said anything yet:
 * either way there is nothing to read.
 */
export async function logTail(key: RunKey, opts?: { tail?: number }): Promise<LogLine[]> {
  const run = await current(key);
  if (!run) return [];

  const signal = AbortSignal.timeout(LOG_READ_TIMEOUT_MS);
  const res = await client.openDevRunLogs(run.userId, run.id, {
    tail: opts?.tail ?? LOG_TAIL,
    follow: false,
    signal,
  });
  if (!res.ok) throw new Error(res.error);

  const out: LogLine[] = [];
  for await (const text of lines(res.data, signal)) {
    out.push({ seq: out.length, text });
  }
  // The read carries its own deadline, so a fired timeout left the document partial.
  // Say so in the output itself rather than passing it off as complete.
  if (signal.aborted) {
    out.push({
      seq: out.length,
      text: `… log read timed out after ${Math.round(LOG_READ_TIMEOUT_MS / 1000)}s; output truncated`,
    });
  }
  return out;
}
