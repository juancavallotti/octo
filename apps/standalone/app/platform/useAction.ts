"use client";

import { useCallback, useState } from "react";

/**
 * One button's call to the shell: whether it is in flight, and what went wrong when
 * it did. The shell's own refusals are sentences meant for the user, so they are
 * shown as they come; Electron prefixes them with the channel, which is not.
 */
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
    setBusy(true);
    setError(null);
    try {
      return await fn();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      if (message !== "Cancelled") setError(cleanMessage(message));
      return undefined;
    } finally {
      setBusy(false);
    }
  }, []);

  return { busy, error, run, clearError: () => setError(null) };
}

/** An IPC error as the user should read it. */
export function cleanMessage(message: string): string {
  return message.replace(/^Error invoking remote method '[^']+':\s*(Error:\s*)?/, "");
}
