"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  deleteAgentUserMemory,
  deleteMemoryThread,
  listAgentUserMemories,
  listMemoryAgents,
  listMemoryIntegrations,
  listMemoryThreads,
  readMemoryThread,
  readMemoryWorking,
  searchAgentMemory,
  type MemoryAgent,
  type MemoryHit,
  type MemoryThread,
  type MemoryTranscript,
  type UserMemory,
  type WorkingMemory,
} from "@/app/model/agentMemory";
import type { Integration } from "@/app/model/orchestrator";
import { MemoryPickers } from "./MemoryPickers";
import { MemorySearch } from "./MemorySearch";
import { SearchRanking } from "./SearchRanking";
import { ThreadList } from "./ThreadList";
import { ThreadTranscript } from "./ThreadTranscript";
import { WorkingMemoryPanel } from "./WorkingMemoryPanel";
import { UserMemoryList } from "./UserMemoryList";

/**
 * What an agent remembers, for an operator.
 *
 * Read and delete only. There is no way to edit a remembered fact here, and that
 * is deliberate: an operator rewriting what an agent believes about a person,
 * with no audit trail and nothing in the conversation explaining the change, is a
 * feature that should be asked for explicitly.
 *
 * It is a top-level platform section rather than a page under an integration
 * because an operator opening this is usually asking "what does this agent know",
 * not "what does this one integration know" — and because the integrations route
 * is an optional catch-all that a nested page would collide with. It is not under
 * Admin either: admin is settings that belong to the installation, and this is
 * data that belongs to integrations, read the way logs and traces are.
 */
export default function AgentMemoryManager() {
  const confirm = useConfirm();
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [integrationId, setIntegrationId] = useState("");
  const [agents, setAgents] = useState<MemoryAgent[]>([]);
  const [agentId, setAgentId] = useState("");
  const [threads, setThreads] = useState<MemoryThread[]>([]);
  const [selected, setSelected] = useState<MemoryTranscript | null>(null);
  const [working, setWorking] = useState<WorkingMemory | null>(null);
  const [memories, setMemories] = useState<UserMemory[]>([]);
  const [hits, setHits] = useState<MemoryHit[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const fail = useCallback((e: unknown) => setError((e as Error).message), []);

  useEffect(() => {
    listMemoryIntegrations().then(setIntegrations, fail);
  }, [fail]);

  // The current selection, readable from a callback that started before it
  // changed. State alone cannot serve that: a closure captures the value it was
  // created with, which is exactly the stale value a late response has to be
  // compared against.
  // Written by the two change handlers, beside the state they mirror, rather than
  // during render — React reserves render for producing output, and a ref written
  // there is a side effect the linter is right to refuse.
  const integrationRef = useRef(integrationId);
  const agentRef = useRef(agentId);

  const loadThreads = useCallback(
    (integration: string, agent: string) => {
      if (!integration || !agent) {
        setThreads([]);
        return;
      }
      listMemoryThreads(integration, agent).then((page) => {
        if (integration !== integrationRef.current || agent !== agentRef.current) return;
        setThreads(page.threads);
      }, fail);
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
    integrationRef.current = next;
    agentRef.current = "";
    setIntegrationId(next);
    setAgentId("");
    setAgents([]);
    setThreads([]);
    setSelected(null);
    setWorking(null);
    setMemories([]);
    setHits(null);
    if (next) listMemoryAgents(next).then(setAgents, fail);
  };

  /** Choosing an agent clears what belonged to the last one and loads its threads. */
  const changeAgent = (next: string) => {
    agentRef.current = next;
    setAgentId(next);
    setSelected(null);
    setWorking(null);
    setMemories([]);
    setHits(null);
    loadThreads(integrationId, next);
  };

  /**
   * Read one conversation, and what the agent remembers about the person in it.
   *
   * The selection is captured before the first await and checked after each one.
   * Without that, switching agent while a read is in flight lets the late
   * response overwrite the cleared state — and the viewer then shows one agent's
   * conversation and one person's remembered facts under a different agent's
   * name, which is the worst kind of wrong for a tool whose whole job is telling
   * you what a particular agent knows.
   */
  const openThread = async (threadKey: string) => {
    const forIntegration = integrationId;
    const forAgent = agentId;
    const stale = () => forIntegration !== integrationRef.current || forAgent !== agentRef.current;

    setBusy(true);
    setError(null);
    setWorking(null);
    try {
      // The transcript and the live context are read together and shown together,
      // because the interesting fact is the DIFFERENCE between them: one is
      // uncompacted and the other is whatever survived compaction. In parallel,
      // since neither read depends on the other and this is a page someone is
      // waiting on.
      const [transcript, live] = await Promise.all([
        readMemoryThread(forIntegration, forAgent, threadKey),
        readMemoryWorking(forIntegration, forAgent, threadKey),
      ]);
      if (stale()) return;
      setSelected(transcript);
      setWorking(live);
      // A conversation names the person it was with, which is the only handle the
      // curated memories are addressed by — so they can only be loaded once one is
      // open. An agent that serves nobody in particular has none.
      if (transcript.thread.userId) {
        const next = await listAgentUserMemories(forIntegration, forAgent, transcript.thread.userId);
        if (stale()) return;
        setMemories(next);
      } else {
        setMemories([]);
      }
    } catch (e) {
      if (!stale()) fail(e);
    } finally {
      if (!stale()) setBusy(false);
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
    const forIntegration = integrationId;
    const forAgent = agentId;
    try {
      const found = await searchAgentMemory(forIntegration, forAgent, text);
      if (forIntegration !== integrationRef.current || forAgent !== agentRef.current) return;
      setHits(found);
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
        <SearchRanking />

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
                {!busy && <WorkingMemoryPanel working={working} />}
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
