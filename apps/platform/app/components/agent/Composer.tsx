"use client";

import { Send, Square } from "lucide-react";

import { useAutoGrow } from "./useAutoGrow";

/**
 * How many lines the box grows to before it starts scrolling instead. Four: a
 * question worth asking is usually longer than a line, and every line past that
 * is taken from the transcript above.
 */
const MAX_ROWS = 4;

/** The message box, and the keyboard conventions that go with it. */
export default function Composer({
  draft,
  onDraft,
  onSubmit,
  busy,
  onStop,
}: {
  draft: string;
  onDraft: (value: string) => void;
  onSubmit: () => void;
  /** A run is in flight. */
  busy: boolean;
  onStop: () => void;
}) {
  // The one rule both ways in are held to: a draft that is only whitespace is not
  // a message, whether it is sent by button or by Enter.
  const submit = () => {
    if (draft.trim()) onSubmit();
  };

  // The height is owned by the hook, so there is no max-height class below — a
  // class and a measured cap would be two answers to one question.
  const box = useAutoGrow(draft, MAX_ROWS);

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      className="flex items-end gap-2 border-t border-black/10 px-3 py-2 dark:border-white/10"
    >
      <textarea
        ref={box}
        value={draft}
        rows={1}
        placeholder="Ask Dr. Octo…"
        aria-label="Message"
        onChange={(e) => onDraft(e.target.value)}
        onKeyDown={(e) => {
          // Enter sends, shift+enter breaks the line — except mid-composition,
          // where an IME uses Enter to accept the candidate it is offering.
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            submit();
          }
        }}
        className="min-h-[2rem] flex-1 resize-none rounded-md border border-black/10 bg-transparent px-2 py-1.5 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
      />
      {busy && (
        <button
          type="button"
          onClick={onStop}
          title="Stop"
          aria-label="Stop"
          className="rounded-md bg-zinc-200 p-2 text-zinc-700 hover:bg-zinc-300 dark:bg-zinc-700 dark:text-zinc-100 dark:hover:bg-zinc-600"
        >
          <Square size={14} />
        </button>
      )}
      <button
        type="submit"
        disabled={!draft.trim()}
        title="Send"
        aria-label="Send"
        className="rounded-md bg-sky-600 p-2 text-white transition-colors hover:bg-sky-500 disabled:opacity-40"
      >
        <Send size={14} />
      </button>
    </form>
  );
}
