"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { MessagesSquare, Search, UserRound } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { TabStrip, tabPanelProps, type TabDef } from "@/app/components/TabStrip";
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
import { ConversationDetail } from "./ConversationDetail";
import { FactsPanel } from "./FactsPanel";
import { MemoryPickers } from "./MemoryPickers";
import { SearchPanel } from "./SearchPanel";
import { ThreadList } from "./ThreadList";

/**
 * What an agent remembers, for an operator.
 *
 * Read and delete only. There is no way to edit a remembered fact here, and that
 * is deliberate: an operator rewriting what an agent believes about a person, with
 * no audit trail and nothing in the conversation explaining the change, is a
 * feature that should be asked for explicitly.
 *
 * It is a top-level platform section rather than a page under an integration
 * because an operator opening this is usually asking "what does this agent know",
 * not "what does this one integration know" — and because the integrations route
 * is an optional catch-all that a nested page would collide with. It is not under
 * Admin either: admin is settings that belong to the installation, and this is
 * data that belongs to integrations, read the way logs and traces are.
 *
 * The shape is the object store's, and for the same reasons: the pickers that
 * scope everything sit in one compact row above the tabs, and the page fills the
 * width rather than sitting in a column. What is on this page is transcripts and
 * search hits — lines of prose, not fields — and a narrow column turned every one
 * of them into four wrapped lines.
 *
 * Three tabs, because there are three questions and only one of them is about a
 * particular conversation: what was said, what is remembered about somebody, and
 * where is that thing I remember. Search used to sit above the list, where having
 * results pushed the conversation out of view.
 */
type Tab = "conversations" | "facts" | "search";

const TABS: readonly TabDef<Tab>[] = [
  { id: "conversations", label: "Conversations", icon: MessagesSquare },
  { id: "facts", label: "Facts", icon: UserRound },
  { id: "search", label: "Search", icon: Search },
];

export default function AgentMemoryManager() {
  const confirm = useConfirm();
  const [tab, setTab] = useState<Tab>("conversations");
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
  // Written by the two change handlers, beside the state they mirror, rather than
  // during render — React reserves render for producing output, and a ref written
  // there is a side effect the linter is right to refuse.
  const integrationRef = useRef(integrationId);
  const agentRef = useRef(agentId);

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
        if (integration !== integrationRef.current || agent !== agentRef.current) return;
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
    const stale = () => forIntegration !== integrationRef.current || forAgent !== agentRef.current;

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
      if (stale()) return;
      setSelected(transcript);
      setWorking(live);
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
      if (forIntegration !== integrationRef.current || forAgent !== agentRef.current) return;
      setHits(found);
    } catch (e) {
      fail(e);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <MemoryPickers
        integrations={integrations}
        integrationId={integrationId}
        onIntegrationChange={changeIntegration}
        agents={agents}
        agentId={agentId}
        onAgentChange={changeAgent}
      />

      {error && (
        <p className="shrink-0 border-b border-black/10 px-4 py-2 text-sm text-red-500 dark:border-white/10">
          {error}
        </p>
      )}

      {!agentId ? (
        <div className="flex min-h-0 flex-1 items-center justify-center p-8">
          <p className="max-w-md text-center text-sm text-zinc-500 dark:text-zinc-400">
            Choose an integration and an agent to read what it has recorded: the
            conversations it has had, and the facts it kept about the people in them.
            Conversations are stored uncompacted, so this is what was actually said —
            not the shortened version the model still carries.
          </p>
        </div>
      ) : (
        <>
          <TabStrip
            tabs={TABS}
            selected={tab}
            onSelect={setTab}
            label="Agent memory views"
            idPrefix="memory"
          />

          {/* Every panel stays mounted: switching tabs must not drop the open
              conversation, the chosen person, or a set of search results somebody
              is working through. */}
          <div
            {...tabPanelProps("memory", "conversations", tab === "conversations")}
            className={
              tab === "conversations" ? "flex min-h-0 flex-1 overflow-hidden" : "hidden"
            }
          >
            <div className="w-80 shrink-0 overflow-y-auto border-r border-black/10 p-3 dark:border-white/10">
              <ThreadList
                threads={threads}
                selected={selected?.thread.threadKey ?? null}
                onOpen={openThread}
                onDelete={removeThread}
              />
            </div>
            <ConversationDetail transcript={selected} working={working} busy={busy} />
          </div>

          <div
            {...tabPanelProps("memory", "facts", tab === "facts")}
            className={tab === "facts" ? "flex min-h-0 flex-1 flex-col" : "hidden"}
          >
            <FactsPanel
              people={people}
              userId={userId}
              onUserChange={changeUser}
              memories={memories}
              onForget={forget}
            />
          </div>

          <div
            {...tabPanelProps("memory", "search", tab === "search")}
            className={tab === "search" ? "flex min-h-0 flex-1 flex-col" : "hidden"}
          >
            <SearchPanel onSearch={search} hits={hits} onOpen={openThread} />
          </div>
        </>
      )}
    </div>
  );
}
