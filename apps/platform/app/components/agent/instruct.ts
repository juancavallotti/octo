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
 * Both come back empty and immediately. What the run DID with the instruction
 * arrives on the stream it already owns, which is a better account than anything
 * invented here — so this reports only whether the instruction was delivered, and
 * never guesses at the outcome.
 *
 * That much has to be reported, though. A caller that cannot tell a delivered
 * instruction from a refused one shows the same thing for both, and for an
 * authorization the two look identical from the reader's chair: a click that
 * failed and a click never made are both followed by silence and then a denial on
 * the timeout. A stop can ignore the answer — the run it ends is one this panel
 * has already stopped reading.
 */

import { readThreadId } from "./thread";

/**
 * Post one instruction on the reader's current conversation, reporting whether it
 * was delivered.
 *
 * False covers everything that stops it reaching the run: a refusal from the
 * route (an expired session is the one to expect), and a request that never
 * completed at all.
 */
export async function post(
  userKey: string,
  instruction: Record<string, unknown>,
): Promise<boolean> {
  try {
    const res = await fetch("/api/agent/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ threadId: readThreadId(userKey), message: "", ...instruction }),
    });
    await res.body?.cancel();
    return res.ok;
  } catch {
    return false;
  }
}
