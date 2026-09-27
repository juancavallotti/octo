"use client";

import { useCallback, useRef, useState } from "react";

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
  /** Report why the files are staying put, without touching them. */
  hold: (reason: string) => void;
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
  /**
   * What is on the message, read synchronously.
   *
   * The limits are about the message, and reading them off React state cannot
   * enforce that: `add` reads each file with an await, so two drops in quick
   * succession both see the state as it was before either of them finished and
   * both decide there is room. A ref is updated the moment a decision is taken,
   * so the second drop sees the first one's files even though React has not
   * rendered them yet.
   *
   * It is the authority for the limit check; the state is what renders.
   */
  const held = useRef<Attachment[]>([]);

  const commit = useCallback((next: Attachment[]) => {
    held.current = next;
    setFiles(next);
  }, []);

  const add = useCallback(
    (picked: File[]) => {
      void (async () => {
        let reason: string | null = null;
        for (const file of picked) {
          // Checked twice, and the second one is the one that counts.
          //
          // This one is the cheap refusal: a type the model cannot read, or a file
          // over the per-file limit, decided before spending the read on it.
          //
          // The first reason, not the last: a person fixes one thing at a time,
          // and the first refusal is the one they can act on.
          const refusal = acceptable(file, held.current, accepted);
          if (refusal) {
            reason ??= refusal;
            continue;
          }
          const read = await readAttachment(file);
          // And this one decides. It is synchronous with the commit below — no
          // await between them — which is what makes the pair atomic: a
          // concurrent drop can commit while this one is reading a file off disk,
          // so a decision taken before the read has already gone stale by the
          // time it is acted on.
          const late = acceptable(file, held.current, accepted);
          if (late) {
            reason ??= late;
            continue;
          }
          commit([...held.current, read]);
        }
        setRejected(reason);
      })();
    },
    [accepted, commit],
  );

  const remove = useCallback(
    (id: string) => commit(held.current.filter((a) => a.id !== id)),
    [commit],
  );

  const clear = useCallback(() => {
    commit([]);
    setRejected(null);
  }, [commit]);

  const hold = useCallback((reason: string) => setRejected(reason), []);

  return { files, rejected, add, remove, clear, hold };
}
