/**
 * Dr. Octo's past conversations, as the panel shows them.
 *
 * They come from the orchestrator's agent-memory tables, which are keyed on the
 * integration and survive a redeploy. The mapping onto the wire types below
 * happens here.
 */

import { type ActionResult } from "@octo/http";
import { DR_OCTO_AGENT_ID } from "@/app/agent/identity";
import { fetchAgentStatus } from "./agentUrl";
import { deleteThread, listThreads, readThread, type MemoryTurn } from "./agentMemory";

// Re-exported for the callers that already name it here.
export { DR_OCTO_AGENT_ID };

/** Shown for a conversation the agent chose not to name. */
const UNTITLED = "Untitled conversation";

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
    data: result.data.threads.map((t) => {
      // A conversation with no name is one the agent decided was not worth
      // naming — a greeting, a test message.
      const id = threadIdOf(t.threadKey, user.id);
      return { id, title: t.title || UNTITLED, updatedAt: t.lastActivityAt };
    }),
  };
}

/**
 * The thread id a conversation is addressed by, out of its stored key.
 *
 * Dr. Octo keys a conversation on the authenticated user AND the thread, so a
 * stolen thread id names a conversation that does not exist. The prefix has to
 * come off again here: handing back a composed key gets it composed a second time
 * — `{user}/{user}/{thread}` — and the next message silently starts a new
 * conversation beside the one on screen.
 */
function threadIdOf(threadKey: string, userId: string): string {
  const prefix = `${userId}/`;
  return threadKey.startsWith(prefix) ? threadKey.slice(prefix.length) : threadKey;
}

/** The stored key for a conversation the panel addresses by thread id. */
function threadKeyOf(threadId: string, userId: string): string {
  return `${userId}/${threadId}`;
}

/** One past conversation, to replay into the panel. */
export async function readConversation(
  user: Asker,
  threadId: string,
): Promise<ActionResult<Conversation>> {
  const integration = await integrationId();
  if (!integration.ok) return integration;

  // A conversation that is not there comes back as an error rather than as an
  // empty one: rows are only opened from a listing just fetched, so a miss is a
  // case that genuinely went wrong.
  const result = await readThread(
    integration.id,
    DR_OCTO_AGENT_ID,
    threadKeyOf(threadId, user.id),
  );
  if (!result.ok) return result;

  // Scoped to the asker here rather than in the query, because the route is
  // addressed by thread and a conversation belongs to one person. Someone reading
  // a thread key that is not theirs gets nothing.
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

/**
 * Erase a conversation: its turns, its working memory and the conversation itself.
 *
 * Scoped the way a read is — the key is composed from the asker — so the worst a
 * wrong thread id can do is name a conversation that does not exist.
 */
export async function deleteConversation(
  user: Asker,
  threadId: string,
): Promise<ActionResult<void>> {
  const integration = await integrationId();
  if (!integration.ok) return integration;

  return deleteThread(integration.id, DR_OCTO_AGENT_ID, threadKeyOf(threadId, user.id));
}

/**
 * The marker Dr. Octo's own `input` expression puts between the question and the
 * context it appends to it. A literal here because it is a literal there.
 */
const CONTEXT_MARKER = "\n\n---\nContext, not part of the question.";

/**
 * Map a stored turn onto the two roles rendered, dropping the context Dr. Octo's
 * own `input` expression appended to the question.
 *
 * Trimmed here and only here: agent memory stores what was sent and returns it as
 * sent, and the shape being trimmed is the one his definition built.
 */
function toTurn(turn: MemoryTurn): ConversationTurn {
  const cut = turn.text.indexOf(CONTEXT_MARKER);
  const text = cut === -1 ? turn.text : turn.text.slice(0, cut).trimEnd();
  return { role: turn.role === "user" ? "user" : "agent", text };
}

type IntegrationResult = { ok: true; id: string } | { ok: false; error: string };

/**
 * Which integration Dr. Octo is installed as.
 *
 * Read from his status rather than configured: it is whatever the install
 * produced. Not cached here — the status lookup behind it already is.
 */
async function integrationId(): Promise<IntegrationResult> {
  const status = await fetchAgentStatus();
  if (!status?.integrationId) {
    return { ok: false, error: "the agent is not installed" };
  }
  return { ok: true, id: status.integrationId };
}
