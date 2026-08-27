"use server";

/**
 * Server actions for the agent memory viewer.
 *
 * Read and delete only. There is deliberately no action to EDIT a curated
 * memory: an operator rewriting what an agent believes about a person, with no
 * audit trail and nothing in the conversation explaining it, is a feature that
 * should be asked for explicitly rather than fall out of a viewer.
 *
 * The reads are behind the write gate rather than the read one. What is here is a
 * transcript of people's conversations with an agent and a list of facts it has
 * kept about them — the same material the chat panel is gated on, for the same
 * reason.
 */

import { withWrite } from "./_auth";
import * as client from "./client/agentMemory";
import { listIntegrations as listIntegrationsClient } from "./client/integrations";
import type { ActionResult } from "@octo/http";
import type {
  MemoryAgent,
  MemoryHit,
  MemoryThreadPage,
  MemoryTranscript,
  ThreadQuery,
  UserMemory,
  WorkingMemory,
} from "./client/agentMemory";
import type { Integration } from "@/app/model/orchestrator";

/**
 * Every integration, so the viewer can offer somewhere to look.
 *
 * Behind the write gate like everything else here, and not the read one. It is a
 * listing of every integration on the installation by name, reached from a page
 * whose whole purpose is reading people's conversations — admitting a reader to
 * the picker while refusing them everything it picks would be a gap rather than a
 * concession.
 */
export async function listMemoryIntegrations(): Promise<ActionResult<Integration[]>> {
  return withWrite(() => listIntegrationsClient());
}

export async function listMemoryAgents(
  integrationId: string,
): Promise<ActionResult<MemoryAgent[]>> {
  return withWrite(() => client.listMemoryAgents(integrationId));
}

export async function listMemoryThreads(
  integrationId: string,
  agentId: string,
  query?: ThreadQuery,
): Promise<ActionResult<MemoryThreadPage>> {
  return withWrite(() => client.listThreads(integrationId, agentId, query));
}

export async function readMemoryThread(
  integrationId: string,
  agentId: string,
  threadKey: string,
): Promise<ActionResult<MemoryTranscript>> {
  return withWrite(() => client.readThread(integrationId, agentId, threadKey));
}

export async function readMemoryWorking(
  integrationId: string,
  agentId: string,
  threadKey: string,
): Promise<ActionResult<WorkingMemory>> {
  return withWrite(() => client.readWorkingMemory(integrationId, agentId, threadKey));
}

export async function deleteMemoryThread(
  integrationId: string,
  agentId: string,
  threadKey: string,
): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteThread(integrationId, agentId, threadKey));
}

export async function listAgentUserMemories(
  integrationId: string,
  agentId: string,
  userId: string,
): Promise<ActionResult<UserMemory[]>> {
  return withWrite(() => client.listUserMemories(integrationId, agentId, userId));
}

export async function deleteAgentUserMemory(
  integrationId: string,
  agentId: string,
  userId: string,
  name: string,
): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteUserMemory(integrationId, agentId, userId, name));
}

export async function searchAgentMemory(
  integrationId: string,
  agentId: string,
  text: string,
): Promise<ActionResult<MemoryHit[]>> {
  return withWrite(() => client.searchMemory(integrationId, agentId, text));
}
