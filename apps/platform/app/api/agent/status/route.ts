export const runtime = "nodejs";
export const dynamic = "force-dynamic";

import { currentWriteUserId } from "@/app/actions/_auth";
import { fetchAgentStatus } from "../resolve";

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
  const available = Boolean(status && status.state === "deployed" && status.internalUrl);
  return Response.json({ available });
}
