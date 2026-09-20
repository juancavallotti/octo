"use client";

import { useCallback, useState } from "react";

import { acceptable, readAttachment, type Attachment } from "./attachments";

export interface Attachments {
  /** The files on the message being written. */
  files: Attachment[];
  /** Why the last attempt to add one went nowhere, or null. */
  rejected: string | null;
  /** Read picked files and keep the ones that fit. */
  add: (picked: File[]) => void;
  remove: (id: string) => void;
  /** Called once the message is sent. */
  clear: () => void;
}

/**
 * The files a message is carrying, and the limits they are held to.
 *
 * Separate from the panel because it is a small state machine with a rule in it
 * rather than layout, and because what it enforces — how many files, how large
 * together — is about the *message*, which is exactly what the panel is not.
 */
export function useAttachments(accepted: readonly string[]): Attachments {
  const [files, setFiles] = useState<Attachment[]>([]);
  const [rejected, setRejected] = useState<string | null>(null);

  const add = useCallback(
    (picked: File[]) => {
      void (async () => {
        let reason: string | null = null;
        const added: Attachment[] = [];
        for (const file of picked) {
          // Checked against what is already there *plus* what this drop has
          // already taken — the limits are about the message, so five files that
          // each fit can still be four too many, and reading the React state here
          // would let every file in one drop pass the same "is there room" test.
          const refusal = acceptable(file, [...files, ...added], accepted);
          if (refusal) {
            // The first reason, not the last: a person fixes one thing at a time,
            // and the first refusal is the one they can act on.
            reason ??= refusal;
            continue;
          }
          added.push(await readAttachment(file));
        }
        if (added.length) setFiles((prev) => [...prev, ...added]);
        setRejected(reason);
      })();
    },
    [files, accepted],
  );

  const remove = useCallback(
    (id: string) => setFiles((prev) => prev.filter((a) => a.id !== id)),
    [],
  );

  const clear = useCallback(() => {
    setFiles([]);
    setRejected(null);
  }, []);

  return { files, rejected, add, remove, clear };
}
