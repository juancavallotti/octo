/**
 * What a conversation looks like once the frames have been folded together, and
 * the fold itself.
 *
 * Separate from the hook because it is the part with no React in it: given a turn
 * and a frame it returns the next turn, which is the whole of what the panel
 * renders and the easiest thing in this feature to test.
 */

import type { AgentEvent } from "./events";

/** One tool the agent called, and how it went. */
export interface ToolRun {
  id: string;
  tool: string;
  done: boolean;
  failed: boolean;
  /** The arguments the model chose, and what came back. Shown on expand. */
  input?: unknown;
  output?: unknown;
}

/** One turn in the transcript. A user turn carries only text. */
export interface Turn {
  id: string;
  role: "user" | "agent";
  text: string;
  tools: ToolRun[];
  /**
   * The model's reasoning, accumulated as it arrives. Usually far longer than the
   * answer, and the only thing there is to show before the answer begins.
   */
  thinking: string;
  /** Set when the agent declined the question or the run failed. */
  note?: string;
  streaming: boolean;
}

/** The runtime's guardrail reasons, said in a way a reader can act on. */
const GUARDRAIL_NOTES: Record<string, string> = {
  "model refused": "He declined this one.",
  "exceeded max iterations":
    "He ran out of steps before finishing. Try narrowing the question, or raise AGENT_MAX_ITERATIONS on his deployment.",
};

/** Fold one frame into a turn. */
export function reduce(turn: Turn, event: AgentEvent): Turn {
  switch (event.type) {
    case "text":
      return { ...turn, text: turn.text + event.text };

    case "thinking":
      return { ...turn, thinking: turn.thinking + event.text };

    case "tool_call":
      return {
        ...turn,
        tools: [
          ...turn.tools,
          {
            id: event.toolCallId,
            tool: event.tool,
            done: false,
            failed: false,
            input: event.input,
          },
        ],
      };

    case "tool_result":
      return {
        ...turn,
        tools: turn.tools.map((run) =>
          run.id === event.toolCallId
            ? { ...run, done: true, failed: Boolean(event.isError), output: event.output }
            : run,
        ),
      };

    // The final answer, which on a streaming run is the text already accumulated.
    // Taken only when nothing streamed, so a non-streaming agent still shows one.
    case "done":
      return turn.text ? turn : { ...turn, text: event.text ?? "" };

    case "error":
      return { ...turn, note: event.error };

    // The reason is diagnostic and written for a log — "model refused",
    // "exceeded max iterations". The *reply* to the user comes from the
    // guardrail's own set-payload, and reaches the panel as the closing frame.
    case "guardrail":
      return { ...turn, note: GUARDRAIL_NOTES[event.reason ?? ""] ?? "He stopped short of an answer." };
  }
}
