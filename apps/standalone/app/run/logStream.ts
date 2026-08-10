import { snapshot, subscribe } from "./session";
import type { LogLine, LogStreamOptions } from "@octo/run-host";

/**
 * Replay a namespace's log buffer and then follow it, as one stream.
 *
 * It sits apart from the runner because it touches none of what the runner owns:
 * no child process, no ports, no config file — only the buffer a session already
 * holds. What it does own is the handover from replay to live, which is the part
 * with a correctness argument worth keeping in one place.
 *
 * The subscription is taken BEFORE the snapshot, deliberately. Both happen in one tick
 * today, so nothing can arrive between them — but ordering it this way means that if
 * anything ever does, the line is delivered twice rather than lost, and the sequence
 * cursor below drops the duplicate. A dropped log line is invisible; a repeated one is
 * not.
 *
 * The cursor does double duty: it honours `fromSeq` so an SSE reconnect does not replay
 * what the client already showed, and it makes that handover safe.
 */
export async function* followLogs(
  ns: string,
  opts: LogStreamOptions,
): AsyncGenerator<LogLine> {
  let lastSeq = opts.fromSeq ?? -1;

  const pending: LogLine[] = [];
  let wake: (() => void) | null = null;
  const nudge = () => {
    const resume = wake;
    wake = null;
    resume?.();
  };

  const unsubscribe = subscribe(ns, (line) => {
    pending.push(line);
    nudge();
  });
  // An abort has to wake the loop as well as end it: without this the generator would
  // stay parked on the promise below, holding its subscription, until a line happened to
  // arrive for a client that has already gone.
  opts.signal.addEventListener("abort", nudge, { once: true });

  try {
    for (const line of snapshot(ns)) {
      if (line.seq <= lastSeq) continue;
      lastSeq = line.seq;
      yield line;
    }
    while (!opts.signal.aborted) {
      const line = pending.shift();
      if (line === undefined) {
        await new Promise<void>((resolve) => {
          wake = resolve;
        });
        continue;
      }
      if (line.seq <= lastSeq) continue;
      lastSeq = line.seq;
      yield line;
    }
  } finally {
    opts.signal.removeEventListener("abort", nudge);
    unsubscribe();
  }
}
