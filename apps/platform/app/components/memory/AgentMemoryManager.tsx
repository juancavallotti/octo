"use client";

import { MessagesSquare, Search, UserRound } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { TabStrip, tabPanelProps, type TabDef } from "@/app/components/TabStrip";
import { ConversationDetail } from "./ConversationDetail";
import { FactsPanel } from "./FactsPanel";
import { MemoryPickers } from "./MemoryPickers";
import { SearchPanel } from "./SearchPanel";
import { ThreadList } from "./ThreadList";
import { useAgentMemory, type MemoryTab } from "./useAgentMemory";

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
 *
 * The selection cascade and every read live in useAgentMemory; this renders.
 */
const TABS: readonly TabDef<MemoryTab>[] = [
  { id: "conversations", label: "Conversations", icon: MessagesSquare },
  { id: "facts", label: "Facts", icon: UserRound },
  { id: "search", label: "Search", icon: Search },
];

export default function AgentMemoryManager() {
  const m = useAgentMemory(useConfirm());

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <MemoryPickers
        integrations={m.integrations}
        integrationId={m.integrationId}
        onIntegrationChange={m.changeIntegration}
        agents={m.agents}
        agentId={m.agentId}
        onAgentChange={m.changeAgent}
      />

      {m.error && (
        <p className="shrink-0 border-b border-black/10 px-4 py-2 text-sm text-red-500 dark:border-white/10">
          {m.error}
        </p>
      )}

      {!m.agentId ? (
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
            selected={m.tab}
            onSelect={m.setTab}
            label="Agent memory views"
            idPrefix="memory"
          />

          {/* Every panel stays mounted: switching tabs must not drop the open
              conversation, the chosen person, or a set of search results somebody
              is working through. */}
          <div
            {...tabPanelProps("memory", "conversations", m.tab === "conversations")}
            className={m.tab === "conversations" ? "flex min-h-0 flex-1 overflow-hidden" : "hidden"}
          >
            <div className="w-80 shrink-0 overflow-y-auto border-r border-black/10 p-3 dark:border-white/10">
              <ThreadList
                threads={m.threads}
                selected={m.selected?.thread.threadKey ?? null}
                onOpen={m.openThread}
                onDelete={m.removeThread}
              />
            </div>
            <ConversationDetail transcript={m.selected} working={m.working} busy={m.busy} />
          </div>

          <div
            {...tabPanelProps("memory", "facts", m.tab === "facts")}
            className={m.tab === "facts" ? "flex min-h-0 flex-1 flex-col" : "hidden"}
          >
            <FactsPanel
              people={m.people}
              userId={m.userId}
              onUserChange={m.changeUser}
              memories={m.memories}
              onForget={m.forget}
            />
          </div>

          <div
            {...tabPanelProps("memory", "search", m.tab === "search")}
            className={m.tab === "search" ? "flex min-h-0 flex-1 flex-col" : "hidden"}
          >
            <SearchPanel onSearch={m.search} hits={m.hits} onOpen={m.openThread} />
          </div>
        </>
      )}
    </div>
  );
}
