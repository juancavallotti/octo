"use client";

import { useState } from "react";
import type { WorkingMemory } from "@/app/model/agentMemory";

/**
 * What the agent still carries, beside what it actually said.
 *
 * The transcript next to this one is kept uncompacted forever. Working memory is
 * not: it is pruned or summarized to stay inside the model's context window, so
 * it is the only place you can see what an agent has FORGOTTEN. A conversation
 * whose transcript runs to forty turns and whose working memory holds six is a
 * conversation where the model no longer knows how it started — and until this
 * panel existed there was no way to tell that from the outside, which made
 * "it doesn't remember what I told it" impossible to confirm or refute.
 *
 * The payload is the runtime's own serialized form. The orchestrator stores it
 * without parsing it — deliberately, so the engine can change the format without
 * a schema migration — so this decodes it BEST EFFORT: the shape it recognizes is
 * rendered as messages, and anything else falls back to the raw text with the
 * counts still shown. It is a viewer, not a parser, and it must not be the reason
 * the engine cannot change its format.
 */
export function WorkingMemoryPanel({ working }: { working: WorkingMemory | null }) {
  const [raw, setRaw] = useState(false);

  if (!working) return null;

  if (!working.found) {
    return (
      <Frame>
        <p className="p-4 text-sm text-zinc-500 dark:text-zinc-400">
          This conversation carries no live context. That is ordinary: one that ended
          cleanly keeps its transcript and nothing to resume from.
        </p>
      </Frame>
    );
  }

  const messages = raw ? null : decode(working.payload);

  return (
    <Frame>
      <div className="flex items-baseline justify-between gap-4 border-b border-black/10 px-4 py-3 dark:border-white/10">
        <div>
          <h2 className="text-sm font-medium">Working memory</h2>
          <p className="mt-0.5 text-xs text-zinc-500 dark:text-zinc-400">
            {working.tokens.toLocaleString()} tokens &middot; {formatBytes(working.bytes)} &middot;
            iteration {working.iteration} &middot; version {working.version}
          </p>
        </div>
        {working.readable && (
          <button
            type="button"
            onClick={() => setRaw((r) => !r)}
            className="shrink-0 text-xs text-zinc-500 underline underline-offset-2 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-100"
          >
            {raw ? "Show messages" : "Show raw"}
          </button>
        )}
      </div>

      {!working.readable ? (
        <p className="p-4 text-sm text-zinc-500 dark:text-zinc-400">
          The runtime stored {formatBytes(working.bytes)} in a form that is not text, so
          there is nothing to show here. The counts above still describe it.
        </p>
      ) : messages ? (
        <ol className="flex flex-col gap-3 p-4">
          {messages.map((m, i) => (
            <li key={i} className="text-sm">
              <span className="mb-0.5 flex flex-wrap items-center gap-2 text-xs font-medium uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
                {m.role}
                {m.tools.map((tool) => (
                  <span
                    key={tool}
                    className="rounded bg-black/5 px-1.5 py-0.5 font-mono text-[10px] normal-case tracking-normal dark:bg-white/10"
                  >
                    {tool}
                  </span>
                ))}
              </span>
              {m.text ? (
                <p className="whitespace-pre-wrap break-words">{m.text}</p>
              ) : (
                m.tools.length === 0 && (
                  <p className="text-zinc-500 italic dark:text-zinc-400">no text</p>
                )
              )}
            </li>
          ))}
          {messages.length === 0 && (
            <li className="text-sm text-zinc-500 dark:text-zinc-400">
              The agent is carrying no messages into its next turn.
            </li>
          )}
        </ol>
      ) : (
        <pre className="overflow-x-auto p-4 font-mono text-xs break-words whitespace-pre-wrap">
          {working.payload}
        </pre>
      )}
    </Frame>
  );
}

/**
 * The panel's shell, and a labelled landmark.
 *
 * The label earns its place: this panel and the transcript beside it deliberately
 * show overlapping text — that is the comparison — so "which panel is this in" is
 * a real question for a screen reader and for a test, and neither should have to
 * answer it by position.
 */
function Frame({ children }: { children: React.ReactNode }) {
  return (
    <section
      aria-label="Working memory"
      className="rounded-lg border border-black/10 dark:border-white/10"
    >
      {children}
    </section>
  );
}

/** One message as this viewer renders it, whatever the envelope called it. */
interface Carried {
  role: string;
  text: string;
  /** Tools named on this message, either called or answered. */
  tools: string[];
}

/**
 * Read the runtime's envelope, or return null and let the caller show the raw
 * text.
 *
 * Null rather than a thrown error, and null for anything unrecognized rather
 * than a partial render: the format belongs to the engine, and a viewer that
 * guessed at a shape it did not recognize would show an operator something
 * confidently wrong about what an agent remembers. Falling back to the raw
 * payload is always truthful.
 *
 * Both capitalizations are accepted because the envelope serializes Go structs
 * whose fields are exported, so `Role`/`Text` is what actually lands on disk —
 * while the wire types everywhere else in this app are lowercase. Taking both
 * costs one `??` and saves this from breaking on a tag change that means nothing.
 */
function decode(payload?: string): Carried[] | null {
  if (!payload) return null;
  try {
    const parsed: unknown = JSON.parse(payload);
    if (!parsed || typeof parsed !== "object") return null;
    const messages = (parsed as { messages?: unknown }).messages;
    if (!Array.isArray(messages)) return null;
    return messages.map((m) => {
      const entry = (m ?? {}) as Record<string, unknown>;
      return {
        role: String(entry.Role ?? entry.role ?? "unknown"),
        text: String(entry.Text ?? entry.text ?? ""),
        tools: [
          ...toolNames(entry.ToolCalls ?? entry.toolCalls),
          ...toolNames(entry.ToolResults ?? entry.toolResults),
        ],
      };
    });
  } catch {
    return null;
  }
}

/**
 * The tool names on a call or result list.
 *
 * Worth pulling out rather than leaving as an empty line: a tool result carries
 * its content in a structured field and nothing in `Text`, so without this a
 * conversation full of tool round-trips renders as a column of blank entries —
 * and tool traffic is often most of what fills an agent's context.
 */
function toolNames(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value
    .map((entry) => {
      const t = (entry ?? {}) as Record<string, unknown>;
      return String(t.Name ?? t.name ?? t.Tool ?? t.tool ?? "");
    })
    .filter(Boolean);
}

/** Bytes as an operator reads them. */
function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
