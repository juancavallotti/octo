export const runtime = "nodejs";
export const dynamic = "force-dynamic";

import { currentWriteUserId } from "@/app/actions/_auth";
import { AuthError, ForbiddenError } from "@/app/auth/guard";
import { resolveAgentUrl, forgetAgentUrl, orchestratorUrl } from "../resolve";

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
 */
export async function POST(req: Request) {
  if (!orchestratorUrl()) {
    return Response.json(
      { error: "orchestrator not configured (ORCHESTRATOR_URL unset)" },
      { status: 503 },
    );
  }

  let body: Record<string, unknown>;
  try {
    body = (await req.json()) as Record<string, unknown>;
  } catch {
    return Response.json({ error: "invalid request body" }, { status: 400 });
  }

  // The write roles, not merely a session. Dr. Octo holds full read-write access to
  // the orchestrator API, so a chat route open to any signed-in user would be a way
  // around the gate every other write on this platform goes through.
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

  const agent = await resolveAgentUrl();
  if (!agent.ok) {
    return Response.json({ error: agent.error }, { status: agent.status });
  }

  // The identity is written *over* whatever arrived, last, so a forged user block
  // in the request cannot survive. It is the only field the client does not own:
  // the agent keys its memory on the user id, so accepting one from the browser
  // would let anyone read anyone's conversation by asking for it.
  const payload = { ...body, user };

  let upstream: Response;
  try {
    upstream = await fetch(`${agent.url}/chat`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
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

