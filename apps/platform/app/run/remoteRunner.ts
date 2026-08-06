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
 * The platform's {@link AppRunner}: the app the editor is running lives in a dev-run pod
 * the orchestrator owns, not in this process.
 *
 * This exists because the local runner cannot work here. Its state is a process handle, a
 * port from a pod-local pool and a log buffer in one heap, and the platform runs several
 * replicas with no session affinity — so a status call landing on a different replica than
 * the start did found none of it and reported "not running". Nothing in this module holds
 * state at all: every operation derives what it needs from (user, integration) by asking
 * the orchestrator, which reads the cluster. Any replica can therefore serve any request,
 * which is the whole of the fix.
 *
 * Two consequences worth knowing before reading further:
 *
 *   - **The run has no id here.** A dev run's id is derived from (user, integration) by an
 *     HMAC the orchestrator keys, so this side cannot compute it and must not cache it.
 *     Operations that need one look the run up first — a label lookup against an informer
 *     cache, which is why paying for it twice is cheaper than remembering it once.
 *   - **Nothing here pushes config.** The run's sidecar pulls the *saved* definition, so
 *     `yaml` is ignored throughout and a draft cannot be run at all. That is the drift
 *     from standalone that {@link RunState.reloadsOnSave} announces to the editor.
 */

/** How much log history the orchestrator replays before it starts tailing. */
const LOG_TAIL = 500;

/** What a caller is told when the editor asks to run something it has never saved. */
export const UNSAVED =
  "Save this integration before running it — the running app reads the saved definition.";

/**
 * The state of no run. Reported rather than an error: "nothing is running for this
 * integration" is the ordinary answer on every editor mount, and there is no stored row
 * that could have said otherwise.
 *
 * `reloadsOnSave` is true even here, because it describes the backend and not the run —
 * the RUN panel has to explain how reloading works before anything is running.
 */
const NOT_RUNNING: RunState = {
  running: false,
  exposable: false,
  port: null,
  testUrl: null,
  reloadsOnSave: true,
};

/**
 * The run's address, or null when this key cannot name one.
 *
 * A dev run is (user, integration) and nothing else. The namespace on the key — the whole
 * of the local runner's identity — is deliberately unused: a dev pod is not shared with
 * anyone, so there is nothing for a per-browser slug to separate.
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
    // A dev run that exists is a run, whatever phase its pod is in. There is no
    // "stopped but remembered" state to distinguish it from: Stop deletes the workload,
    // and a deleted workload is simply absent from the list above.
    running: true,
    // The orchestrator publishes an endpoint exactly when the definition declares an
    // HTTP_PORT, so having a host to advertise IS being networked.
    exposable: !!run.testUrl,
    // Always null: a dev pod owns its network namespace and listens on a platform
    // constant, so there is no allocated port for anyone to record or reconcile.
    port: null,
    // Withheld until the run is ready, deliberately. The endpoint is published as soon
    // as the run is created, so offering the URL earlier would hand the user a link that
    // answers 502 while the pod pulls its image.
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
  // An abort has to reach the socket, not just end the loop: without this the reader
  // stays parked on a follow that will never produce another line for a client that has
  // already gone, holding the connection to the orchestrator open behind it.
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
    // A final line with no newline after it is still a line: the container was killed
    // mid-write, and dropping it would silently lose the run's last word — often the
    // interesting one.
    buffered += decoder.decode();
    if (buffered !== "") yield buffered;
  } finally {
    signal.removeEventListener("abort", cancel);
    // Cancel rather than release: releasing a reader with a read still outstanding
    // throws, and a generator closed early — the client went away — always has one.
    cancel();
  }
}

/**
 * Follow the run's logs, numbering each line for the client's de-duplication.
 *
 * The numbering starts *after* `fromSeq` rather than from zero, and that matters on a
 * reconnect: the client drops anything at or below the last sequence it saw, so numbering
 * from zero again would make it discard fresh lines until the count caught up. Numbering
 * onwards instead means a reconnect shows the replayed tail a second time.
 *
 * That is the deliberate choice, and the only honest one available: a pod log has no
 * cursor to resume from — Kubernetes offers "the last N lines" and nothing that says
 * "after the line I already had" — so the alternatives are a repeated line or a lost one.
 * A repeated line is visible; a lost one is not.
 */
async function* followLogs(
  key: RunKey,
  opts: LogStreamOptions,
): AsyncGenerator<LogLine> {
  const run = await current(key);
  if (!run || opts.signal.aborted) return;

  const res = await client.openDevRunLogs(run.userId, run.id, {
    tail: LOG_TAIL,
    signal: opts.signal,
  });
  if (!res.ok) {
    // An abort is how this stream normally ends, and it surfaces here as a failed
    // request: the fetch was cancelled. Not a failure to report.
    if (opts.signal.aborted) return;
    // Anything else is: a run whose pod is not scheduled yet has no logs, and the
    // orchestrator says so rather than pretending to an empty stream. Raised so the
    // caller's stream ends and its client retries, which is what a starting run needs.
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
   * The reload on an attach is what makes clicking Run mean "run what is saved now" even
   * when a save's own reload notification never landed — the orchestrator's notify is
   * best-effort by design, since a save must not fail because a pod was unreachable. A
   * fresh pod needs no reload: its sidecar's first pull IS the load.
   *
   * That reload is best-effort too, for the same reason it is best-effort there: the run
   * is up either way, and refusing a Run because a reload did not land would turn a stale
   * generation into no run at all.
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
   * Reload now — the explicit gesture, not the per-edit one. The editor does not push
   * edits to this backend at all (see `reloadsOnSave`), so this is a user asking directly
   * for the saved state to be picked up.
   *
   * The state returned describes the run as it was *before* the reload, and cannot do
   * otherwise: a reload's effect is the sidecar pulling and the runtime re-reading its
   * directory, both of which happen after this returns. The caller polls status for the
   * state after it.
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
