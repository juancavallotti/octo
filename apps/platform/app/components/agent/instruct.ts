"use client";

/**
 * Instructions addressed to the conversation rather than to a stream.
 *
 * A stop and an authorization are the same kind of thing: a request that says
 * something about a run already in flight and expects nothing back. The runtime
 * routes both by the conversation, so they reach the run wherever it is being
 * worked on — including on a replica this browser never spoke to, which is what
 * hanging up cannot do.
 *
 * Both come back empty and immediately. What the run DID with the instruction
 * arrives on the stream it already owns, so this reports only whether the
 * instruction was delivered and never guesses at the outcome.
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
