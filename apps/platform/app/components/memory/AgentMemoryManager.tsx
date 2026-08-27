"use client";

import { useCallback, useEffect, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  deleteAgentUserMemory,
  deleteMemoryThread,
  listAgentUserMemories,
  listMemoryAgents,
  listMemoryIntegrations,
  listMemoryThreads,
  readMemoryThread,
  searchAgentMemory,
  type MemoryAgent,
  type MemoryHit,
  type MemoryThread,
  type MemoryTranscript,
  type UserMemory,
} from "@/app/model/agentMemory";
import type { Integration } from "@/app/model/orchestrator";
import { MemoryPickers } from "./MemoryPickers";
import { MemorySearch } from "./MemorySearch";
import { ThreadList } from "./ThreadList";
import { ThreadTranscript } from "./ThreadTranscript";
import { UserMemoryList } from "./UserMemoryList";

/**
 * What an agent remembers, for an operator.
 *
 * Read and delete only. There is no way to edit a remembered fact here, and that
 * is deliberate: an operator rewriting what an agent believes about a person,
 * with no audit trail and nothing in the conversation explaining the change, is a
 * feature that should be asked for explicitly.
 *
 * It lives in the admin section rather than under an integration because an
 * operator opening this is usually asking "what does this agent know", not
 * "what does this one integration know" — and because the integrations route is
 * an optional catch-all that a nested page would collide with.
 */
export default function AgentMemoryManager() {
  const confirm = useConfirm();
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [integrationId, setIntegrationId] = useState("");
  const [agents, setAgents] = useState<MemoryAgent[]>([]);
  const [agentId, setAgentId] = useState("");
  const [threads, setThreads] = useState<MemoryThread[]>([]);
  const [selected, setSelected] = useState<MemoryTranscript | null>(null);
  const [memories, setMemories] = useState<UserMemory[]>([]);
  const [hits, setHits] = useState<MemoryHit[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const fail = useCallback((e: unknown) => setError((e as Error).message), []);

  useEffect(() => {
    listMemoryIntegrations().then(setIntegrations, fail);
  }, [fail]);

  const loadThreads = useCallback(
    (integration: string, agent: string) => {
      if (!integration || !agent) {
        setThreads([]);
        return;
      }
      listMemoryThreads(integration, agent).then((page) => setThreads(page.threads), fail);
    },
    [fail],
  );

  /**
   * Choosing an integration clears everything below it and reloads the agents.
   *
   * The cascade lives in the handler rather than in an effect on integrationId,
   * because it is a response to something someone did — not a synchronization
   * with an external system. An effect would also mean the stale agent list, the
   * open conversation and the search results all survive for one render after the
   * change, which is exactly long enough to be visible.
   *
   * Agents come from what has actually been stored rather than from parsing
   * definitions: one appears here once it has remembered something, which is when
   * there is anything to look at.
   */
  const changeIntegration = (next: string) => {
    setIntegrationId(next);
    setAgentId("");
    setAgents([]);
    setThreads([]);
    setSelected(null);
    setMemories([]);
    setHits(null);
    if (next) listMemoryAgents(next).then(setAgents, fail);
  };

  /** Choosing an agent clears what belonged to the last one and loads its threads. */
  const changeAgent = (next: string) => {
    setAgentId(next);
    setSelected(null);
    setMemories([]);
    setHits(null);
    loadThreads(integrationId, next);
  };

  const openThread = async (threadKey: string) => {
    setBusy(true);
    setError(null);
    try {
      const transcript = await readMemoryThread(integrationId, agentId, threadKey);
      setSelected(transcript);
      // A conversation names the person it was with, which is the only handle the
      // curated memories are addressed by — so they can only be loaded once one is
      // open. An agent that serves nobody in particular has none.
      if (transcript.thread.userId) {
        setMemories(await listAgentUserMemories(integrationId, agentId, transcript.thread.userId));
      } else {
        setMemories([]);
      }
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  const removeThread = async (threadKey: string) => {
    const ok = await confirm({
      title: "Erase this conversation?",
      body:
        "Everything goes: what was said, and the working state the agent would have " +
        "resumed from. This cannot be undone.",
      confirmLabel: "Erase",
      danger: true,
    });
    if (!ok) return;
    try {
      await deleteMemoryThread(integrationId, agentId, threadKey);
      if (selected?.thread.threadKey === threadKey) setSelected(null);
      loadThreads(integrationId, agentId);
    } catch (e) {
      fail(e);
    }
  };

  const forget = async (name: string) => {
    const userId = selected?.thread.userId;
    if (!userId) return;
    const ok = await confirm({
      title: `Forget “${name}”?`,
      body: "The agent will no longer carry this into later conversations with this person.",
      confirmLabel: "Forget",
      danger: true,
    });
    if (!ok) return;
    try {
      await deleteAgentUserMemory(integrationId, agentId, userId, name);
      setMemories(await listAgentUserMemories(integrationId, agentId, userId));
    } catch (e) {
      fail(e);
    }
  };

  const search = async (text: string) => {
    if (!text.trim()) {
      setHits(null);
      return;
    }
    try {
      setHits(await searchAgentMemory(integrationId, agentId, text));
    } catch (e) {
      fail(e);
    }
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-4xl">
        <h1 className="text-lg font-semibold">Agent memory</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          What an agent has recorded: the conversations it has had, and the facts it chose
          to keep about the people in them. Conversations are stored uncompacted, so this
          is what was actually said — not the shortened version the model still carries.
        </p>

        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}

        <MemoryPickers
          integrations={integrations}
          integrationId={integrationId}
          onIntegrationChange={changeIntegration}
          agents={agents}
          agentId={agentId}
          onAgentChange={changeAgent}
        />

        {agentId && (
          <>
            <MemorySearch onSearch={search} hits={hits} onOpen={openThread} />
            <div className="mt-4 grid gap-4 lg:grid-cols-[20rem_1fr]">
              <ThreadList
                threads={threads}
                selected={selected?.thread.threadKey ?? null}
                onOpen={openThread}
                onDelete={removeThread}
              />
              <div className="flex flex-col gap-4">
                <ThreadTranscript transcript={selected} busy={busy} />
                {selected?.thread.userId && (
                  <UserMemoryList
                    userId={selected.thread.userId}
                    memories={memories}
                    onForget={forget}
                  />
                )}
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
