"use client";

/**
 * Instructions addressed to the conversation rather than to a stream.
 *
 * A stop and an authorization are the same kind of thing: a request that says
 * something about a run already in flight and expects nothing back. The runtime
 * routes both by the conversation, so they reach the run wherever it is being
 * worked on — including through a proxy that has not noticed this browser's
 * socket go, and on a replica this browser never spoke to. That is exactly what
 * hanging up cannot do.
 *
 * Both come back empty and immediately, and both are deliberately silent about
 * failure: what happened arrives on the stream the run already owns, which is a
 * better account than anything invented here. A stop that did not land leaves a
 * run this panel has already stopped reading; an authorization that did not land
 * is denied by the run's own clock, and the run says so.
 */

import { readThreadId } from "./thread";

/** Post one instruction on the reader's current conversation. */
export function post(userKey: string, instruction: Record<string, unknown>): void {
  void fetch("/api/agent/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ threadId: readThreadId(userKey), message: "", ...instruction }),
  })
    .then((res) => res.body?.cancel())
    .catch(() => {});
}
