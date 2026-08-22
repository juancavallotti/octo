"use client";

import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { AlertTriangle } from "lucide-react";
import type { Segment, Turn } from "./turns";
import ThinkingSegment from "./ThinkingSegment";
import ToolsSegment from "./ToolsSegment";
import CompactionNotice from "./CompactionNotice";
import SignalNotice from "./SignalNotice";

/**
 * One turn in the transcript.
 *
 * The agent is told to answer in Markdown, so this renders it — which is what
 * makes a flow definition readable in a chat panel at all. `react-markdown` builds
 * React elements rather than HTML, so nothing here reaches `dangerouslySetInnerHTML`
 * and a model that emits a `<script>` tag renders it as the text it is. That is the
 * point: the answer is generated from content other people wrote.
 */

/** The renderers that need styling. Everything else takes the prose defaults below. */
const COMPONENTS = {
  a: (props: React.ComponentProps<"a">) => (
    // Answers cite external documentation, and a model can be wrong about a link.
    // noreferrer keeps this platform's URLs out of whatever it points at.
    <a {...props} target="_blank" rel="noreferrer noopener" className="text-sky-600 underline dark:text-sky-400" />
  ),
  code: ({ className, children, ...props }: React.ComponentProps<"code">) => {
    // react-markdown gives a fenced block a language class and an inline span none,
    // which is the only way to tell them apart.
    const fenced = /language-/.test(className ?? "");
    if (fenced) {
      return (
        <code {...props} className={`${className ?? ""} block font-mono text-xs`}>
          {children}
        </code>
      );
    }
    return (
      <code {...props} className="rounded bg-black/[0.06] px-1 py-0.5 font-mono text-[0.85em] dark:bg-white/10">
        {children}
      </code>
    );
  },
  pre: (props: React.ComponentProps<"pre">) => (
    // Overflow rather than wrap: a wrapped YAML block is not copyable as YAML.
    <pre
      {...props}
      className="my-2 overflow-x-auto rounded-md border border-black/10 bg-black/[0.03] p-2 dark:border-white/10 dark:bg-white/[0.04]"
    />
  ),
  table: (props: React.ComponentProps<"table">) => (
    <div className="my-2 overflow-x-auto">
      <table {...props} className="w-full border-collapse text-xs" />
    </div>
  ),
  th: (props: React.ComponentProps<"th">) => (
    <th {...props} className="border border-black/10 px-2 py-1 text-left font-medium dark:border-white/10" />
  ),
  td: (props: React.ComponentProps<"td">) => (
    <td {...props} className="border border-black/10 px-2 py-1 align-top dark:border-white/10" />
  ),
  ul: (props: React.ComponentProps<"ul">) => <ul {...props} className="my-1.5 list-disc pl-5" />,
  ol: (props: React.ComponentProps<"ol">) => <ol {...props} className="my-1.5 list-decimal pl-5" />,
  p: (props: React.ComponentProps<"p">) => <p {...props} className="my-1.5 first:mt-0 last:mb-0" />,
  h1: (props: React.ComponentProps<"h1">) => <h1 {...props} className="mt-3 mb-1 text-sm font-semibold" />,
  h2: (props: React.ComponentProps<"h2">) => <h2 {...props} className="mt-3 mb-1 text-sm font-semibold" />,
  h3: (props: React.ComponentProps<"h3">) => <h3 {...props} className="mt-3 mb-1 text-sm font-semibold" />,
};

export default function AgentMessage({ turn }: { turn: Turn }) {
  if (turn.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-lg rounded-br-sm bg-sky-600 px-3 py-1.5 text-sm text-white">
          {turn.segments.map((segment, i) =>
            segment.kind === "text" ? <span key={i}>{segment.text}</span> : null,
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      {/* Rendered in the order they happened, which is the whole reason a turn is
          a list. Index keys are safe here because segments are only ever appended
          — nothing is inserted, removed or reordered. */}
      {turn.segments.map((segment, i) => (
        <SegmentView
          key={i}
          segment={segment}
          streaming={turn.streaming}
          last={i === turn.segments.length - 1}
        />
      ))}

      {/* Nothing has arrived yet, so the panel is never silent between sending and
          the first sign of work. */}
      {turn.streaming && turn.segments.length === 0 && (
        <span className="inline-block h-3 w-1.5 animate-pulse rounded-sm bg-zinc-400" />
      )}

      {turn.note && (
        <p className="flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400">
          <AlertTriangle size={13} className="mt-px shrink-0" />
          {turn.note}
        </p>
      )}
    </div>
  );
}

/** One segment, rendered as whatever it is. */
function SegmentView({
  segment,
  streaming,
  last,
}: {
  segment: Segment;
  /** The run is still going. */
  streaming: boolean;
  /** Nothing has come after this one yet. */
  last: boolean;
}) {
  switch (segment.kind) {
    case "thinking":
      // "Answered" is now positional rather than a property of the turn: reasoning
      // is the main event only while nothing has followed it, which is exactly
      // what being the last segment means.
      return <ThinkingSegment text={segment.text} streaming={streaming} answered={!last} />;

    case "tools":
      return <ToolsSegment runs={segment.runs} />;

    case "compaction":
      return <CompactionNotice done={segment.done} dropped={segment.dropped} />;

    case "signal":
      return <SignalNotice signal={segment.signal} text={segment.text} />;

    case "text":
      return (
        <div className="text-sm text-zinc-800 dark:text-zinc-100">
          <Markdown remarkPlugins={[remarkGfm]} components={COMPONENTS}>
            {segment.text}
          </Markdown>
        </div>
      );
  }
}
