"use client";

import { useEffect, useRef, useState } from "react";
import { Brain, ChevronDown, ChevronRight } from "lucide-react";

/**
 * The model's reasoning while it is reasoning.
 *
 * This exists because leaving it out made the panel look broken: on a reasoning
 * model most of the output is thinking — 875 of 900 tokens on the conversation
 * that prompted this — so without it there is nothing on screen between the
 * question and the answer, and a model that is working is indistinguishable from
 * one that has hung.
 *
 * Open while it streams, closed once the answer starts. That ordering is the whole
 * design: the reasoning is worth watching when it is the only thing happening, and
 * is clutter the moment there is an answer to read instead.
 */
export default function ThinkingPanel({
  text,
  streaming,
  answered,
}: {
  text: string;
  /** The run is still going. */
  streaming: boolean;
  /** The answer has begun, so the reasoning is no longer the main event. */
  answered: boolean;
}) {
  // Derived rather than synchronised: until someone touches it the panel simply
  // *is* open-while-unanswered, which needs no effect and cannot lag a render
  // behind. After a deliberate choice it follows that choice, so the answer
  // arriving never collapses a panel somebody is reading.
  const [choice, setChoice] = useState<boolean | null>(null);
  const open = choice ?? !answered;
  const tail = useRef<HTMLDivElement>(null);

  // Follow the reasoning as it arrives, so the newest line is the visible one.
  // Optional-called: this is a nicety, and not every environment implements it.
  useEffect(() => {
    if (open) tail.current?.scrollIntoView?.({ block: "nearest" });
  }, [text, open]);

  if (!text) return null;
  const Chevron = open ? ChevronDown : ChevronRight;

  return (
    <div className="rounded-md border border-black/[0.08] bg-black/[0.02] dark:border-white/10 dark:bg-white/[0.03]">
      <button
        type="button"
        onClick={() => setChoice(!open)}
        aria-expanded={open}
        className="flex w-full items-center gap-1.5 px-2 py-1.5 text-left text-[11px] font-medium text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Chevron size={12} className="shrink-0" />
        <Brain
          size={12}
          className={`shrink-0 ${streaming && !answered ? "animate-pulse text-violet-500" : ""}`}
        />
        <span>{streaming && !answered ? "Thinking…" : "Thought about it"}</span>
        {!open && (
          <span className="ml-auto font-mono text-[10px] tabular-nums opacity-60">
            {/* A rough size, so a collapsed panel still says how much is behind it. */}
            {text.length.toLocaleString()} chars
          </span>
        )}
      </button>

      {open && (
        <div className="max-h-40 overflow-y-auto px-2 pb-2">
          <p className="text-[11px] leading-relaxed whitespace-pre-wrap text-zinc-500 dark:text-zinc-400">
            {text}
          </p>
          <div ref={tail} />
        </div>
      )}
    </div>
  );
}
