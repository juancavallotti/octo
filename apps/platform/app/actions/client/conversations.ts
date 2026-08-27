/**
 * Dr. Octo's past conversations, as the panel shows them.
 *
 * These used to reach the agent's own pod: the runtime had nowhere to keep a
 * durable transcript, so the agent recorded one himself into KV and served it
 * from his own flows. That meant reading somebody's history needed the agent
 * deployed and healthy, and it meant the record lived under the deployment that
 * wrote it — so reinstalling him destroyed every conversation on the install.
 *
 * Now it comes from the orchestrator's agent-memory tables, which are keyed on
 * the integration and survive a redeploy. The wire types below are unchanged, so
 * the panel did not have to move with them; the mapping happens here.
 */

import { type ActionResult } from "@octo/http";
import { fetchAgentStatus } from "./agentUrl";
import { listThreads, readThread, type MemoryTurn } from "./agentMemory";

/**
 * The agent id Dr. Octo declares in his own definition.
 *
 * It is a constant here rather than a lookup because it is part of his
 * definition, not of an install: every Dr. Octo is this agent, and an install
 * where it differs is one someone edited, which is supported but not something
 * the panel can guess at.
 */
export const DR_OCTO_AGENT_ID = "dr-octo";

/** One past conversation, as a list shows it. */
export interface ConversationRow {
  id: string;
  title: string;
  /** RFC 3339, written when the conversation was last added to. */
  updatedAt: string;
}

/** One thing that was said. */
export interface ConversationTurn {
  role: "user" | "agent";
  text: string;
}

/** A past conversation, as it was had. */
export interface Conversation {
  threadId: string;
  title: string;
  turns: ConversationTurn[];
}

/** The person asking, which is what either listing is scoped to. */
export interface Asker {
  id: string;
  name: string;
}

/** Every past conversation this person has had, most recently active first. */
export async function listConversations(user: Asker): Promise<ActionResult<ConversationRow[]>> {
  const integration = await integrationId();
  if (!integration.ok) return integration;

  const result = await listThreads(integration.id, DR_OCTO_AGENT_ID, { userId: user.id });
  if (!result.ok) return result;
  return {
    ok: true,
    data: result.data.threads.map((t) => ({
      id: t.threadKey,
      title: t.title || t.threadKey,
      updatedAt: t.lastActivityAt,
    })),
  };
}

/** One past conversation. A thread this person does not have reads as an empty one. */
export async function readConversation(
  user: Asker,
  threadId: string,
): Promise<ActionResult<Conversation>> {
  const integration = await integrationId();
  if (!integration.ok) return integration;

  // A conversation that is not there now comes back as an error rather than as an
  // empty one. That is a change from the KV-backed version, where a missing object
  // read as its default and there was no way to tell "no such conversation" from
  // "a conversation with nothing in it". The panel only opens rows from a listing
  // it has just fetched, so the case is one that genuinely went wrong.
  const result = await readThread(integration.id, DR_OCTO_AGENT_ID, threadId);
  if (!result.ok) return result;
  // Scoped to the asker here rather than in the query, because the route is
  // addressed by thread and a conversation belongs to one person. Someone reading
  // a thread key that is not theirs gets nothing, which is the same answer the
  // agent-served version gave.
  if (result.data.thread.userId && result.data.thread.userId !== user.id) {
    return { ok: true, data: { threadId, title: "", turns: [] } };
  }
  return {
    ok: true,
    data: {
      threadId,
      title: result.data.thread.title ?? "",
      turns: result.data.turns.map(toTurn),
    },
  };
}

/** Map a stored turn onto the two roles the panel renders. */
function toTurn(turn: MemoryTurn): ConversationTurn {
  return { role: turn.role === "user" ? "user" : "agent", text: turn.text };
}

type IntegrationResult = { ok: true; id: string } | { ok: false; error: string; status: number };

/**
 * Which integration Dr. Octo is installed as.
 *
 * Read from his status rather than configured, for the same reason his address
 * is: it is whatever the install produced. Unlike the address it is not cached
 * here — the status lookup behind it already is.
 */
async function integrationId(): Promise<IntegrationResult> {
  const status = await fetchAgentStatus();
  if (!status?.integrationId) {
    return { ok: false, error: "the agent is not installed", status: 503 };
  }
  return { ok: true, id: status.integrationId };
}
