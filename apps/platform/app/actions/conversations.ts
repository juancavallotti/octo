"use server";

/**
 * Server actions for a person's past conversations with Dr. Octo.
 *
 * Reads, and still behind the write gate: the same one the chat route uses, for
 * the same reason. A conversation with Dr. Octo is a record of full read-write
 * orchestrator access being exercised, so admitting a reader here would let
 * someone read what they could not have done.
 *
 * They are actions and not routes because nothing here streams. The chat route is
 * a route because it does.
 */

import { withWriteUser } from "./_auth";
import {
  deleteConversation as erase,
  listConversations as list,
  readConversation as read,
} from "./client/conversations";
import type { ActionResult } from "@octo/http";
import type { Conversation, ConversationRow } from "./client/conversations";

export async function listConversations(): Promise<ActionResult<ConversationRow[]>> {
  return withWriteUser((id, session) => list({ id, name: session.user?.name ?? "" }));
}

export async function readConversation(threadId: string): Promise<ActionResult<Conversation>> {
  return withWriteUser((id, session) => read({ id, name: session.user?.name ?? "" }, threadId));
}

export async function deleteConversation(threadId: string): Promise<ActionResult<void>> {
  return withWriteUser((id, session) => erase({ id, name: session.user?.name ?? "" }, threadId));
}
