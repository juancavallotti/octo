"use client";

/**
 * The integration's name in the detail header, edited in place.
 *
 * It owns the whole editing interaction — the draft, whether the field is open,
 * and the ref that lets Escape cancel without committing — because nothing
 * outside it can see any of that. The caller supplies only `onRename`, and the
 * boolean it returns is what keeps the field open on a rejected name.
 */

import { useRef, useState } from "react";

export interface HeaderNameOptions {
  name: string;
  disabled: boolean;
  /** Returns whether the rename was accepted; false keeps the field open. */
  onRename: (name: string) => Promise<boolean>;
}

export default function HeaderName(options: HeaderNameOptions) {
  const { name, disabled, onRename } = options;

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(name);
  const cancelled = useRef(false);
  const input = useRef<HTMLInputElement>(null);

  const commit = async () => {
    if (cancelled.current) {
      cancelled.current = false;
      setEditing(false);
      return;
    }
    const next = draft.trim();
    if (!next || next === name) {
      setEditing(false);
      return;
    }
    // Keep the editor open until the rename is accepted; a rejected name (e.g. a
    // duplicate) leaves the field open and re-focused so it can be corrected. The
    // error itself is surfaced by the parent's inline banner.
    const ok = await onRename(next);
    if (ok) setEditing(false);
    else input.current?.focus();
  };

  if (editing) {
    return (
      <input
        ref={input}
        autoFocus
        value={draft}
        disabled={disabled}
        aria-label="Integration name"
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
          else if (e.key === "Escape") {
            cancelled.current = true;
            e.currentTarget.blur();
          }
        }}
        className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-1.5 py-0.5 text-base font-semibold outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
      />
    );
  }

  return (
    <button
      type="button"
      onClick={() => {
        setDraft(name);
        setEditing(true);
      }}
      title="Rename integration"
      className="min-w-0 flex-1 truncate text-left text-base font-semibold hover:underline"
    >
      {name}
    </button>
  );
}
