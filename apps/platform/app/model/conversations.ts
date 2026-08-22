/**
 * Browser-side client for a person's past conversations with Dr. Octo. Backed by
 * the server actions in `app/actions/conversations.ts`; these wrappers unwrap the
 * ActionResult so callers keep a value-or-throw contract.
 */

import * as actions from "@/app/actions/conversations";
import { unwrap } from "./bff";

export type {
  Conversation,
  ConversationRow,
  ConversationTurn,
} from "@/app/actions/client/conversations";

import type { Conversation, ConversationRow } from "@/app/actions/client/conversations";

/** Every past conversation, most recently added to first. */
export async function listConversations(): Promise<ConversationRow[]> {
  const rows = await unwrap(await actions.listConversations());
  // Sorted here rather than by the agent: the record appends, and keeping an
  // object sorted that is rewritten on every exchange costs more than sorting a
  // few dozen rows once, when someone opens the list.
  return [...rows].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
}

/** One past conversation, to replay into the panel. */
export async function readConversation(threadId: string): Promise<Conversation> {
  return unwrap(await actions.readConversation(threadId));
}
