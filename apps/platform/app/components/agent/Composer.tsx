"use client";

import { useRef, useState } from "react";
import { Paperclip, Send, Square, X } from "lucide-react";

import { type Attachment, humanSize } from "./attachments";
import { useAutoGrow } from "./useAutoGrow";

/**
 * How many lines the box grows to before it starts scrolling instead. Four: a
 * question worth asking is usually longer than a line, and every line past that
 * is taken from the transcript above.
 */
const MAX_ROWS = 4;

/** What a model that reads no files is refused with, in one place. */
const NO_MEDIA = "This model does not accept attachments.";

/** The message box, and the keyboard conventions that go with it. */
export default function Composer({
  draft,
  onDraft,
  onSubmit,
  busy,
  onStop,
  attachments = [],
  accepted = [],
  onAttach,
  onRemove,
}: {
  draft: string;
  onDraft: (value: string) => void;
  onSubmit: () => void;
  /** A run is in flight. */
  busy: boolean;
  onStop: () => void;
  /** Files already on this message. */
  attachments?: Attachment[];
  /**
   * Content types the site's model reads. Empty means it reads none, and the
   * three ways in all say so rather than doing nothing.
   */
  accepted?: readonly string[];
  onAttach?: (files: File[]) => void;
  onRemove?: (id: string) => void;
}) {
  const [dragging, setDragging] = useState(false);
  // Why the last attempt to attach something went nowhere. Owned here because
  // this is the one refusal the composer can make on its own — every other one
  // needs the file read first, and belongs to whoever reads it.
  const [refused, setRefused] = useState<string | null>(null);
  const picker = useRef<HTMLInputElement>(null);

  const takesFiles = accepted.length > 0 && !!onAttach;

  // The one rule both ways in are held to: a draft that is only whitespace is
  // not a message — unless it carries a file, because "look at this" with no
  // words is a whole question.
  const submit = () => {
    if (draft.trim() || attachments.length) onSubmit();
  };

  const attach = (list: FileList | null) => {
    if (!list?.length || !onAttach) return;
    setRefused(null);
    onAttach(Array.from(list));
  };

  /**
   * The refusal the composer makes itself: a model that reads no files at all.
   *
   * Said out loud rather than swallowed. A screenshot that lands nowhere reads
   * as a broken panel; naming the model says what to change.
   */
  const refuse = () => setRefused(NO_MEDIA);

  // The height is owned by the hook, so there is no max-height class below — a
  // class and a measured cap would be two answers to one question.
  const box = useAutoGrow(draft, MAX_ROWS);

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      onDragOver={(e) => {
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        if (!takesFiles) {
          refuse();
          return;
        }
        attach(e.dataTransfer.files);
      }}
      className={`flex flex-col gap-2 border-t px-3 py-2 ${
        dragging
          ? "border-sky-500 bg-sky-50/50 dark:bg-sky-950/20"
          : "border-black/10 dark:border-white/10"
      }`}
    >
      {refused && (
        <p role="status" className="text-xs text-amber-700 dark:text-amber-400">
          {refused}
        </p>
      )}

      {attachments.length > 0 && (
        <ul aria-label="Attachments" className="flex flex-wrap gap-1.5">
          {attachments.map((file) => (
            <li
              key={file.id}
              className="flex items-center gap-1.5 rounded-md bg-zinc-100 py-1 pl-2 pr-1 text-xs dark:bg-zinc-800"
            >
              <span className="max-w-[12rem] truncate">{file.name}</span>
              <span className="text-zinc-500">{humanSize(file.size)}</span>
              <button
                type="button"
                onClick={() => onRemove?.(file.id)}
                title={`Remove ${file.name}`}
                aria-label={`Remove ${file.name}`}
                className="rounded p-0.5 hover:bg-black/10 dark:hover:bg-white/10"
              >
                <X size={12} />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex items-end gap-2">
        <textarea
          ref={box}
          value={draft}
          rows={1}
          placeholder="Ask Dr. Octo…"
          aria-label="Message"
          onChange={(e) => onDraft(e.target.value)}
          onPaste={(e) => {
            // Only when there are files on the clipboard: taking the event for a
            // plain text paste would break pasting text, which is most pastes.
            if (!e.clipboardData.files.length) return;
            if (!takesFiles) {
              e.preventDefault();
              refuse();
              return;
            }
            e.preventDefault();
            attach(e.clipboardData.files);
          }}
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

        {onAttach && (
          <>
            <input
              ref={picker}
              type="file"
              multiple
              accept={accepted.join(",")}
              // Deliberately unlabelled and hidden from the tree: it is driven by
              // the button beside it, and two controls announcing "Attach files"
              // is one more than a screen reader should find.
              aria-hidden
              tabIndex={-1}
              className="hidden"
              onChange={(e) => {
                attach(e.target.files);
                // Reset, so picking the same file twice in a row still fires a
                // change event.
                e.target.value = "";
              }}
            />
            <button
              type="button"
              onClick={() => (takesFiles ? picker.current?.click() : refuse())}
              // Shown and disabled rather than hidden: a missing button reads as a
              // missing feature, where a disabled one with this title names the
              // thing to change.
              title={takesFiles ? "Attach files" : NO_MEDIA}
              aria-label="Attach files"
              className="rounded-md p-2 text-zinc-600 hover:bg-black/5 disabled:opacity-40 disabled:hover:bg-transparent dark:text-zinc-300 dark:hover:bg-white/10"
            >
              <Paperclip size={14} />
            </button>
          </>
        )}

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
          disabled={!draft.trim() && !attachments.length}
          title="Send"
          aria-label="Send"
          className="rounded-md bg-sky-600 p-2 text-white transition-colors hover:bg-sky-500 disabled:opacity-40"
        >
          <Send size={14} />
        </button>
      </div>
    </form>
  );
}
