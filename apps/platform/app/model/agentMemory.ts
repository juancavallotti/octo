/**
 * Browser-side client for the agent memory viewer. Backed by the server actions
 * in `app/actions/agentMemory.ts`; these wrappers unwrap the ActionResult so
 * callers keep a value-or-throw contract.
 */

import * as actions from "@/app/actions/agentMemory";
import { unwrap } from "./bff";

export type {
  MemoryAgent,
  MemoryHit,
  MemoryThread,
  MemoryThreadPage,
  MemoryTranscript,
  MemoryTurn,
  UserMemory,
  WorkingMemory,
} from "@/app/actions/client/agentMemory";

import type {
  MemoryAgent,
  MemoryHit,
  MemoryThreadPage,
  MemoryTranscript,
  UserMemory,
  WorkingMemory,
} from "@/app/actions/client/agentMemory";
import type { Integration } from "@/app/model/orchestrator";

/** Every integration, to pick one to look at. */
export async function listMemoryIntegrations(): Promise<Integration[]> {
  return unwrap(await actions.listMemoryIntegrations());
}

/** Which agents have memory under an integration. */
export async function listMemoryAgents(integrationId: string): Promise<MemoryAgent[]> {
  return unwrap(await actions.listMemoryAgents(integrationId));
}

/** One page of an agent's conversations, most recently active first. */
export async function listMemoryThreads(
  integrationId: string,
  agentId: string,
  cursor?: string,
): Promise<MemoryThreadPage> {
  return unwrap(await actions.listMemoryThreads(integrationId, agentId, { cursor }));
}

/** One conversation and its turns. */
export async function readMemoryThread(
  integrationId: string,
  agentId: string,
  threadKey: string,
): Promise<MemoryTranscript> {
  return unwrap(await actions.readMemoryThread(integrationId, agentId, threadKey));
}

/**
 * A conversation's working memory: what the agent still carries, as opposed to
 * what it said. `found` is false when there is none, which is not a failure.
 */
export async function readMemoryWorking(
  integrationId: string,
  agentId: string,
  threadKey: string,
): Promise<WorkingMemory> {
  return unwrap(await actions.readMemoryWorking(integrationId, agentId, threadKey));
}

/** Erase a conversation: its working memory, its turns and the conversation itself. */
export async function deleteMemoryThread(
  integrationId: string,
  agentId: string,
  threadKey: string,
): Promise<void> {
  await unwrap(await actions.deleteMemoryThread(integrationId, agentId, threadKey));
}

/** What an agent has kept about one person. */
export async function listAgentUserMemories(
  integrationId: string,
  agentId: string,
  userId: string,
): Promise<UserMemory[]> {
  return unwrap(await actions.listAgentUserMemories(integrationId, agentId, userId));
}

/** Forget one curated memory. */
export async function deleteAgentUserMemory(
  integrationId: string,
  agentId: string,
  userId: string,
  name: string,
): Promise<void> {
  await unwrap(await actions.deleteAgentUserMemory(integrationId, agentId, userId, name));
}

/** Search an agent's conversations and remembered facts. */
export async function searchAgentMemory(
  integrationId: string,
  agentId: string,
  text: string,
): Promise<MemoryHit[]> {
  return unwrap(await actions.searchAgentMemory(integrationId, agentId, text));
}
