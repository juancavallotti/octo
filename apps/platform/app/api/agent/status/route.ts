export const runtime = "nodejs";
export const dynamic = "force-dynamic";

import { currentUserId } from "@/app/actions/_auth";
import { fetchAgentStatus } from "../resolve";

/**
 * GET /api/agent/status — whether the chat launcher should render at all.
 *
 * A route rather than a server action because the launcher sits in the signed-in
 * layout and asks once on mount; and deliberately narrower than the admin page's
 * status, which is a different question asked by a different person. This one
 * answers "can I chat", so it returns a boolean and nothing else — no integration
 * id, no tag, no blocked reason for a reader who cannot act on any of them.
 */
export async function GET() {
  try {
    await currentUserId();
  } catch {
    return Response.json({ error: "unauthenticated" }, { status: 401 });
  }

  const status = await fetchAgentStatus();
  const available = Boolean(status && status.state === "deployed" && status.internalUrl);
  return Response.json({ available });
}
