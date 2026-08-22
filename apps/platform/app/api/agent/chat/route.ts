export const runtime = "nodejs";
export const dynamic = "force-dynamic";

import { currentWriteUserId } from "@/app/actions/_auth";
import { AuthError, ForbiddenError } from "@/app/auth/guard";
import { resolveAgentUrl, forgetAgentUrl, orchestratorUrl } from "@/app/actions/client/agentUrl";

/**
 * POST /api/agent/chat — the browser's end of a conversation with Dr. Octo.
 *
 * Shaped like the pod-log proxy: a raw fetch carrying `req.signal`, the upstream
 * body passed straight through, 499 on abort. Closing the panel therefore aborts
 * the fetch, which aborts this one, which ends the agent's stream — the runtime's
 * `sse-event` sees a closed connection and its `ifClosed: stop` ends the run, so a
 * shut tab does not leave a model running with nobody reading it.
 *
 * The differences from that proxy: it is a POST with a JSON body, the response is
 * `text/event-stream`, and the target is resolved rather than configured.
 *
 * It carries one instruction besides the message. `{stop: true}` becomes the
 * header the agent's `stopWhen` reads, which ends the run on whichever replica is
 * holding the conversation — not only the one this connection reached. Hanging up
 * ends a run too, but only the run whose stream this connection holds.
 */
export async function POST(req: Request) {
  // Authorization first, before anything that would describe this installation to
  // whoever asked. The write roles, not merely a session: Dr. Octo holds full
  // read-write access to the orchestrator API, so a chat route open to any signed-in
  // user would be a way around the gate every other write goes through.
  let user: { id: string; name: string };
  try {
    user = await currentWriteUserId();
  } catch (e) {
    if (e instanceof ForbiddenError) {
      return Response.json({ error: "forbidden" }, { status: 403 });
    }
    if (e instanceof AuthError) {
      return Response.json({ error: "unauthenticated" }, { status: 401 });
    }
    throw e;
  }

  if (!orchestratorUrl()) {
    return Response.json(
      { error: "orchestrator not configured (ORCHESTRATOR_URL unset)" },
      { status: 503 },
    );
  }

  let body: Record<string, unknown>;
  try {
    const parsed: unknown = await req.json();
    // An object, and specifically not the JSON literal `null` — which parses
    // fine and is not an object, so reading a field off it throws and the caller
    // gets a 500 for what is plainly a bad request. Arrays and scalars parse too
    // and carry none of the fields below.
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return Response.json({ error: "invalid request body" }, { status: 400 });
    }
    body = parsed as Record<string, unknown>;
  } catch {
    return Response.json({ error: "invalid request body" }, { status: 400 });
  }

  const agent = await resolveAgentUrl();
  if (!agent.ok) {
    return Response.json({ error: agent.error }, { status: agent.status });
  }

  // The identity is written *over* whatever arrived, last, so a forged user block
  // in the request cannot survive. It is the only field the client does not own:
  // the agent keys its memory on the user id, so accepting one from the browser
  // would let anyone read anyone's conversation by asking for it.
  const payload = { ...body, user };

  // A stop travels as a header because that is what the agent's `stopWhen` reads,
  // and it is set here rather than accepted from the browser for the same reason
  // the identity is: what the client says is a request, and what reaches the agent
  // is this route's decision.
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Accept: "text/event-stream",
  };
  // Strictly true. The panel sends a boolean, and reading anything truthy would
  // let "false" or 0.0 end a run — a stop is the one instruction here that
  // destroys work, so it takes the value it was specified with and no other.
  if (body.stop === true) headers["X-Agent-Stop"] = "1";

  let upstream: Response;
  try {
    upstream = await fetch(`${agent.url}/chat`, {
      method: "POST",
      headers,
      body: JSON.stringify(payload),
      signal: req.signal,
      cache: "no-store",
    });
  } catch (e) {
    if ((e as Error).name === "AbortError") return new Response(null, { status: 499 });
    // The address came from a cached status, and a pod that has since been
    // replaced answers nothing. Drop it so the next attempt resolves again.
    forgetAgentUrl();
    return Response.json({ error: `the agent is unreachable: ${(e as Error).message}` }, { status: 502 });
  }

  if (!upstream.ok || !upstream.body) {
    const text = await upstream.text().catch(() => "");
    return Response.json(
      { error: text || `the agent returned ${upstream.status}` },
      { status: upstream.status || 502 },
    );
  }

  return new Response(upstream.body, {
    headers: {
      "Content-Type": "text/event-stream; charset=utf-8",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      // Nginx and friends buffer a response body by default, which for a token
      // stream means the whole answer arrives at once, at the end.
      "X-Accel-Buffering": "no",
    },
  });
}

