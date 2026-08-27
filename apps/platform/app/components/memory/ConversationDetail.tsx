"use client";

import { useState } from "react";
import { Brain, MessageSquareText } from "lucide-react";
import { TabStrip, tabPanelProps, type TabDef } from "@/app/components/TabStrip";
import type { MemoryTranscript, WorkingMemory } from "@/app/model/agentMemory";
import { ThreadTranscript } from "./ThreadTranscript";
import { WorkingMemoryPanel } from "./WorkingMemoryPanel";

/**
 * One conversation, in its two forms.
 *
 * Tabs rather than one above the other, because these are two versions of the
 * same conversation and the question is always "which one am I reading". Stacked,
 * the second was a scroll away and read as more of the first — which is the exact
 * confusion the pair exists to resolve: the transcript is never compacted and the
 * working memory is whatever survived compaction.
 *
 * Both panels stay mounted. Switching between them is the comparison, and a tab
 * that re-rendered from empty every time would make it a worse one.
 */
type Bucket = "transcript" | "working";

const TABS: readonly TabDef<Bucket>[] = [
  { id: "transcript", label: "Transcript", icon: MessageSquareText },
  { id: "working", label: "Working memory", icon: Brain },
];

export function ConversationDetail({
  transcript,
  working,
  busy,
}: {
  transcript: MemoryTranscript | null;
  working: WorkingMemory | null;
  busy: boolean;
}) {
  const [bucket, setBucket] = useState<Bucket>("transcript");

  // Nothing open: say what to do rather than showing two empty tabs, which read
  // as a conversation that turned out to be empty.
  if (!busy && !transcript) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center p-8">
        <p className="text-sm text-zinc-500 dark:text-zinc-400">
          Choose a conversation to read it.
        </p>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TabStrip
        tabs={TABS}
        selected={bucket}
        onSelect={setBucket}
        label="Conversation views"
        idPrefix="bucket"
      />
      <div
        {...tabPanelProps("bucket", "transcript", bucket === "transcript")}
        className={bucket === "transcript" ? "min-h-0 flex-1 overflow-y-auto p-4" : "hidden"}
      >
        <ThreadTranscript transcript={transcript} busy={busy} />
      </div>
      <div
        {...tabPanelProps("bucket", "working", bucket === "working")}
        className={bucket === "working" ? "min-h-0 flex-1 overflow-y-auto p-4" : "hidden"}
      >
        {busy ? (
          <p className="text-sm text-zinc-500 dark:text-zinc-400">Loading…</p>
        ) : (
          <WorkingMemoryPanel working={working} />
        )}
      </div>
    </div>
  );
}
