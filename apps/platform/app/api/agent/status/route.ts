export const runtime = "nodejs";
export const dynamic = "force-dynamic";

import { currentWriteUserId } from "@/app/actions/_auth";
import { fetchAgentStatus, type AgentReachability } from "@/app/actions/client/agentUrl";

/**
 * GET /api/agent/status — whether the chat launcher should render at all.
 *
 * A route rather than a server action because the launcher sits in the signed-in
 * layout and asks once on mount; and deliberately narrower than the admin page's
 * status, which is a different question asked by a different person. This one
 * answers "can I chat", so it returns a boolean and nothing else — no integration
 * id, no tag, no blocked reason for a reader who cannot act on any of them.
 *
 * It applies the same write-role gate the chat route does, so "available" means
 * available *to you*: a reader who cannot use the panel is not shown a button that
 * would answer 403.
 */
export async function GET() {
  try {
    await currentWriteUserId();
  } catch {
    // Both unauthenticated and forbidden mean the same thing to the launcher, and
    // it renders nothing either way. Reporting which would tell an unauthorized
    // caller whether the agent exists.
    return Response.json({ available: false }, { status: 200 });
  }

  const status = await fetchAgentStatus();
  return Response.json({ available: canChat(status) });
}

/**
 * Whether there is a running agent to talk to.
 *
 * The question is liveness, and `state` answers two things at once: whether a
 * deployment is running, and whether the binary ships a newer bundle than the one
 * rolled out. Gating on `deployed` alone conflated them — upgrading the platform
 * changes the shipped bundle's digest, so a perfectly healthy agent reports
 * `update_available` and the launcher vanished from every page until somebody
 * happened to visit Admin and press Roll out. The chat proxy never agreed with
 * that: it resolves the same status and only ever needed the address, so the panel
 * that was hidden would have worked.
 *
 * So: an address means a deployment the orchestrator could see (Status clears it
 * when the deployment is gone), and `failed` is the one state where the workload
 * is known not to serve. A rollout in progress is deliberately still available —
 * the previous pods are answering until the new ones are ready.
 */
function canChat(status: AgentReachability | null): boolean {
  if (!status || !status.internalUrl) return false;
  return status.state === "deployed" || status.state === "update_available";
}
