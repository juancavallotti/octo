/**
 * What an agent remembers, as typed operations against the orchestrator.
 *
 * This replaces reaching into the agent's own pod for its conversation record.
 * That arrangement existed because the runtime had nowhere to put a durable
 * transcript, so Dr. Octo kept one himself in KV and served it from flows — which
 * meant reading somebody's history required the agent to be deployed and healthy,
 * and meant every agent that wanted history had to build it again.
 *
 * The routes are integration-scoped, because that is what the memory belongs to
 * and what survives a redeploy. Everything else about the layering holds: no verb
 * and no path shape leaves this module.
 */

import { call, enc, type ActionResult } from "./http";

/** One conversation, as a list shows it. */
export interface MemoryThread {
  agentId: string;
  threadKey: string;
  userId?: string;
  title?: string;
  version: number;
  turnCount: number;
  createdAt: string;
  lastActivityAt: string;
}

/** One entry in a conversation's durable record. */
export interface MemoryTurn {
  seq: number;
  role: string;
  text: string;
  tokens?: number;
  attrs?: Record<string, unknown>;
  createdAt: string;
}

/** A page of conversations, with the cursor that continues it. */
export interface MemoryThreadPage {
  threads: MemoryThread[];
  next?: string;
}

/** A conversation and a page of what was said in it. */
export interface MemoryTranscript {
  thread: MemoryThread;
  turns: MemoryTurn[];
  next?: string;
}

/**
 * The live context an interrupted run would resume from.
 *
 * Distinct from the transcript above, and the distinction is the whole point of
 * the feature: turns are kept uncompacted forever, while THIS is what the model
 * actually still carries — pruned or summarized to stay inside the context
 * window. Comparing the two is how you see what an agent has forgotten.
 *
 * `payload` is the runtime's own serialized form. The orchestrator stores it
 * without parsing it, so the engine can change the format without a migration,
 * and `readable` says whether it came back as text at all.
 */
export interface WorkingMemory {
  /** False when the conversation carries no live context, which is ordinary. */
  found: boolean;
  version: number;
  iteration: number;
  tokens: number;
  updatedAt: string;
  bytes: number;
  payload?: string;
  readable: boolean;
}

/** One curated fact an agent kept about a person. */
export interface UserMemory {
  name: string;
  value: string;
  version: number;
  createdAt: string;
  updatedAt: string;
}

/** One agent that has memory under an integration. */
export interface MemoryAgent {
  agentId: string;
  threadCount: number;
  lastActivityAt: string;
}

/** One search result. `kind` says which store it came from. */
export interface MemoryHit {
  kind: "turn" | "user";
  threadKey?: string;
  name?: string;
  text: string;
  seq?: number;
  score: number;
}

/** How a listing is narrowed and continued. */
export interface ThreadQuery {
  userId?: string;
  cursor?: string;
  limit?: number;
}

/** Which agents have memory under an integration. */
export function listMemoryAgents(integrationId: string): Promise<ActionResult<MemoryAgent[]>> {
  return call("GET", `/integrations/${enc(integrationId)}/agent-memory/agents`);
}

/** An agent's conversations, most recently active first. */
export function listThreads(
  integrationId: string,
  agentId: string,
  query: ThreadQuery = {},
): Promise<ActionResult<MemoryThreadPage>> {
  const params = new URLSearchParams();
  if (query.userId) params.set("userId", query.userId);
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.limit) params.set("limit", String(query.limit));
  const suffix = params.size > 0 ? `?${params}` : "";
  return call("GET", `${agentBase(integrationId, agentId)}/threads${suffix}`);
}

/** One conversation and its turns. */
export function readThread(
  integrationId: string,
  agentId: string,
  threadKey: string,
  query: Pick<ThreadQuery, "cursor" | "limit"> = {},
): Promise<ActionResult<MemoryTranscript>> {
  const params = new URLSearchParams();
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.limit) params.set("limit", String(query.limit));
  const suffix = params.size > 0 ? `?${params}` : "";
  return call("GET", `${threadBase(integrationId, agentId, threadKey)}${suffix}`);
}

/**
 * A conversation's working memory.
 *
 * Having none is ordinary rather than exceptional — a conversation that ended
 * cleanly has its transcript and nothing live to resume from — so the route
 * answers 200 with `found: false` and this stays a plain read.
 */
export function readWorkingMemory(
  integrationId: string,
  agentId: string,
  threadKey: string,
): Promise<ActionResult<WorkingMemory>> {
  return call("GET", `${threadBase(integrationId, agentId, threadKey)}/working`);
}

/** Name a conversation. */
export function setThreadTitle(
  integrationId: string,
  agentId: string,
  threadKey: string,
  title: string,
): Promise<ActionResult<void>> {
  return call("PUT", `${threadBase(integrationId, agentId, threadKey)}/title`, { title });
}

/** Erase a conversation: its working memory, its turns and the conversation itself. */
export function deleteThread(
  integrationId: string,
  agentId: string,
  threadKey: string,
): Promise<ActionResult<void>> {
  return call("DELETE", threadBase(integrationId, agentId, threadKey));
}

/** What an agent has kept about one person. */
export function listUserMemories(
  integrationId: string,
  agentId: string,
  userId: string,
): Promise<ActionResult<UserMemory[]>> {
  return call("GET", `${userBase(integrationId, agentId, userId)}`);
}

/**
 * Forget one curated memory.
 *
 * There is deliberately no operation to EDIT one. An operator rewriting what an
 * agent believes about a person, with no audit trail, is a feature that should be
 * asked for explicitly rather than fall out of a viewer.
 */
export function deleteUserMemory(
  integrationId: string,
  agentId: string,
  userId: string,
  name: string,
): Promise<ActionResult<void>> {
  return call("DELETE", `${userBase(integrationId, agentId, userId)}/${enc(name)}`);
}

/** Search an agent's conversations and curated memories. */
export function searchMemory(
  integrationId: string,
  agentId: string,
  text: string,
  opts: { userId?: string; threadKey?: string; limit?: number } = {},
): Promise<ActionResult<MemoryHit[]>> {
  return call("POST", `${agentBase(integrationId, agentId)}/search`, { text, ...opts });
}

function agentBase(integrationId: string, agentId: string): string {
  return `/integrations/${enc(integrationId)}/agent-memory/${enc(agentId)}`;
}

function threadBase(integrationId: string, agentId: string, threadKey: string): string {
  return `${agentBase(integrationId, agentId)}/threads/${enc(threadKey)}`;
}

function userBase(integrationId: string, agentId: string, userId: string): string {
  return `${agentBase(integrationId, agentId)}/users/${enc(userId)}/memories`;
}
