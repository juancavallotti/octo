"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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

/** Which of the three views is showing. */
export type MemoryTab = "conversations" | "facts" | "search";

/**
 * Everything the memory viewer knows and every way it changes, so that the
 * component beside it is only rendering.
 *
 * Split out for the reason the object browser's `useObjects` was: the selection
 * cascades (integration → agent → conversation → person), each step invalidates
 * what is below it, and several reads can be in flight against a selection that
 * has already moved. That is a state machine, and reading it interleaved with
 * JSX made both halves harder to check.
 *
 * Confirmation for destructive actions stays with the caller: asking is a
 * question put to a person, and a hook that opened a dialog would make every test
 * of this logic mount one.
 */
export function useAgentMemory(confirm: (opts: ConfirmOptions) => Promise<boolean>) {
  const [tab, setTab] = useState<MemoryTab>("conversations");
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [integrationId, setIntegrationId] = useState("");
  const [agents, setAgents] = useState<MemoryAgent[]>([]);
  const [agentId, setAgentId] = useState("");
  const [threads, setThreads] = useState<MemoryThread[]>([]);
  const [selected, setSelected] = useState<MemoryTranscript | null>(null);
  const [working, setWorking] = useState<WorkingMemory | null>(null);
  const [userId, setUserId] = useState("");
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
  // Written by the change handlers, beside the state they mirror, rather than
  // during render — React reserves render for producing output, and a ref written
  // there is a side effect the linter is right to refuse.
  const integrationRef = useRef(integrationId);
  const agentRef = useRef(agentId);
  const stale = (forIntegration: string, forAgent: string) =>
    forIntegration !== integrationRef.current || forAgent !== agentRef.current;

  /**
   * Who this agent has talked to, taken from the conversations on hand.
   *
   * Derived rather than fetched because there is no route that enumerates an
   * agent's people, and adding one to populate a dropdown would be a route whose
   * answer is already in a response this page has. The cost is that it covers the
   * page of conversations loaded rather than all of them, which the picker says.
   */
  const people = useMemo(
    () => [...new Set(threads.map((t) => t.userId).filter((u): u is string => !!u))].sort(),
    [threads],
  );

  const loadThreads = useCallback(
    (integration: string, agent: string) => {
      if (!integration || !agent) {
        setThreads([]);
        return;
      }
      listMemoryThreads(integration, agent).then((page) => {
        if (stale(integration, agent)) return;
        setThreads(page.threads);
      }, fail);
    },
    [fail],
  );

  /** Everything below the agent, cleared together. */
  const clearAgentScope = () => {
    setThreads([]);
    setSelected(null);
    setWorking(null);
    setUserId("");
    setMemories([]);
    setHits(null);
  };

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
    clearAgentScope();
    if (next) listMemoryAgents(next).then(setAgents, fail);
  };

  /** Choosing an agent clears what belonged to the last one and loads its threads. */
  const changeAgent = (next: string) => {
    agentRef.current = next;
    setAgentId(next);
    clearAgentScope();
    loadThreads(integrationId, next);
  };

  /** Read what this agent has kept about one person. */
  const changeUser = async (next: string) => {
    setUserId(next);
    setMemories([]);
    if (!next) return;
    try {
      setMemories(await listAgentUserMemories(integrationId, agentId, next));
    } catch (e) {
      fail(e);
    }
  };

  /**
   * Read one conversation: the durable record and the live context together.
   *
   * The selection is captured before the first await and checked after each one.
   * Without that, switching agent while a read is in flight lets the late
   * response overwrite the cleared state — and the viewer then shows one agent's
   * conversation under a different agent's name, which is the worst kind of wrong
   * for a tool whose whole job is telling you what a particular agent knows.
   */
  const openThread = async (threadKey: string) => {
    const forIntegration = integrationId;
    const forAgent = agentId;

    // Opening from a search hit means leaving the results, which is what somebody
    // clicking one is asking for.
    setTab("conversations");
    setBusy(true);
    setError(null);
    setWorking(null);
    try {
      // Read together and shown together, because the interesting fact is the
      // DIFFERENCE between them: one is uncompacted and the other is whatever
      // survived compaction. In parallel, since neither read depends on the other
      // and this is a page someone is waiting on.
      const [transcript, live] = await Promise.all([
        readMemoryThread(forIntegration, forAgent, threadKey),
        readMemoryWorking(forIntegration, forAgent, threadKey),
      ]);
      if (stale(forIntegration, forAgent)) return;
      setSelected(transcript);
      setWorking(live);
    } catch (e) {
      if (!stale(forIntegration, forAgent)) fail(e);
    } finally {
      if (!stale(forIntegration, forAgent)) setBusy(false);
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
      if (selected?.thread.threadKey === threadKey) {
        setSelected(null);
        setWorking(null);
      }
      loadThreads(integrationId, agentId);
    } catch (e) {
      fail(e);
    }
  };

  const forget = async (name: string) => {
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
      if (stale(forIntegration, forAgent)) return;
      setHits(found);
    } catch (e) {
      fail(e);
    }
  };

  return {
    tab, setTab,
    integrations, integrationId, changeIntegration,
    agents, agentId, changeAgent,
    threads, selected, working, busy,
    people, userId, changeUser, memories,
    hits, error,
    openThread, removeThread, forget, search,
  };
}

/** What `useConfirm` takes, named here so the hook does not import the dialog. */
interface ConfirmOptions {
  title: string;
  body: string;
  confirmLabel: string;
  danger: boolean;
}
