/**
 * Dr. Octo's record of what has been said to him, as typed operations.
 *
 * These reach the agent's own deployment rather than the orchestrator, which is
 * why they do not go through `call()` in this folder's transport: that one is
 * bound to ORCHESTRATOR_URL, and the agent's address is resolved per install.
 * Everything else about the layering holds — no verb and no path shape leaves
 * this module, and every failure comes back as an ActionResult.
 */

import { requestJson, type ActionResult } from "@octo/http";
import { resolveAgentUrl, forgetAgentUrl } from "./agentUrl";

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

/** The person asking, which is the only thing either endpoint is keyed on. */
export interface Asker {
  id: string;
  name: string;
}

/**
 * Every past conversation this person has had, most recent last.
 *
 * The order is the recorder's — it appends — and the caller sorts, because a list
 * of a few dozen rows is cheaper to sort in the browser than to keep sorted in an
 * object that is rewritten on every exchange.
 */
export async function listConversations(user: Asker): Promise<ActionResult<ConversationRow[]>> {
  const result = await ask<{ items?: ConversationRow[] }>("/conversations", user);
  if (!result.ok) return result;
  return { ok: true, data: result.data.items ?? [] };
}

/** One past conversation. A thread this person does not have reads as an empty one. */
export function readConversation(user: Asker, threadId: string): Promise<ActionResult<Conversation>> {
  return ask<Conversation>(`/conversations/${encodeURIComponent(threadId)}`, user);
}

/**
 * Post to one of the agent's read endpoints.
 *
 * They are POSTs and not GETs because the identity travels in the body, written
 * server-side exactly as the chat route writes it — the agent keys its record on
 * the user id, so a client that could choose one could read anyone's
 * conversations. Nothing else is sent.
 */
async function ask<T>(path: string, user: Asker): Promise<ActionResult<T>> {
  const agent = await resolveAgentUrl();
  if (!agent.ok) return { ok: false, error: agent.error };

  const result = await requestJson<T>("POST", `${agent.url}${path}`, { user });
  if (!result.ok) {
    // The address came from a cached status, and a pod that has since been
    // replaced answers nothing. Drop it so the next attempt resolves again.
    forgetAgentUrl();
  }
  return result;
}
