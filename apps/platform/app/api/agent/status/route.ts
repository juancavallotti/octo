export const runtime = "nodejs";
export const dynamic = "force-dynamic";

import { currentWriteUserId } from "@/app/actions/_auth";
import { fetchAgentStatus, type AgentReachability } from "@/app/actions/client/agentUrl";

/**
 * GET /api/agent/status — whether the chat launcher should render at all.
 *
 * Narrower than the admin page's status: this one answers "can I chat", so it
 * returns a boolean and nothing else — no integration id, no tag, no blocked
 * reason.
 *
 * It applies the same write-role gate the chat route does, so "available" means
 * available *to you*.
 */
export async function GET() {
  try {
    await currentWriteUserId();
  } catch {
    // Unauthenticated and forbidden answer the same: reporting which would tell an
    // unauthorized caller whether the agent exists.
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
 * rolled out. `update_available` is a healthy agent, so gating on `deployed` alone
 * would hide the launcher from every page after a platform upgrade.
 *
 * So: an address means a deployment the orchestrator could see (Status clears it
 * when the deployment is gone), and `failed` is the one state where the workload
 * is known not to serve. A rollout in progress is still available — the previous
 * pods are answering until the new ones are ready.
 */
function canChat(status: AgentReachability | null): boolean {
  if (!status || !status.internalUrl) return false;
  return status.state === "deployed" || status.state === "update_available";
}
