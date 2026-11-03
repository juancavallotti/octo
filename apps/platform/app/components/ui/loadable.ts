"use client";

import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Data a page fetches, as the three things it can be.
 *
 * Loading is its own state and never inferred from emptiness: an empty list that
 * is still loading and one that has loaded empty want different words on screen,
 * and inferring one from the other leaves a failed first load saying "Loading…"
 * forever underneath the error.
 *
 * A reload keeps what is on screen and says it is `refreshing`, so a poll or the
 * refresh after a mutation never blanks the view back to a skeleton. A reload that
 * fails keeps the data too, with the reason in `error`.
 */
export type Loadable<T> =
  | { status: "loading" }
  | { status: "ready"; data: T; refreshing: boolean; error: string | null }
  | { status: "failed"; error: string };

export interface LoadableHandle<T> {
  state: Loadable<T>;
  /** Read again, keeping the current data on screen meanwhile. */
  reload: () => Promise<void>;
  /** Change the data in place — an optimistic edit ahead of the reload. */
  update: (change: (data: T) => T) => void;
}

/**
 * Load `load()` for `key`, and again whenever the key changes. The key names what
 * is being loaded — an integration id, a filter — and null means not yet: nothing
 * is fetched and the state stays loading. A new key starts from loading, because
 * the old key's data is not this one's.
 */
export function useLoadable<T>(
  key: string | null,
  load: () => Promise<T>,
): LoadableHandle<T> {
  const [held, setHeld] = useState<{ key: string; value: Loadable<T> } | null>(null);
  // The latest load function, so a caller need not memoize it, and a sequence so
  // an answer that lands after a newer request is dropped rather than shown.
  const loadRef = useRef(load);
  useEffect(() => {
    loadRef.current = load;
  });
  const sequence = useRef(0);

  const run = useCallback(async (forKey: string) => {
    const mine = ++sequence.current;
    try {
      const data = await loadRef.current();
      if (sequence.current !== mine) return;
      setHeld({ key: forKey, value: { status: "ready", data, refreshing: false, error: null } });
    } catch (err) {
      if (sequence.current !== mine) return;
      const error = err instanceof Error ? err.message : String(err);
      setHeld((prev) =>
        prev?.key === forKey && prev.value.status === "ready"
          ? { key: forKey, value: { ...prev.value, refreshing: false, error } }
          : { key: forKey, value: { status: "failed", error } },
      );
    }
  }, []);

  useEffect(() => {
    if (key !== null) void run(key);
  }, [key, run]);

  const reload = useCallback(async () => {
    if (key === null) return;
    setHeld((prev) =>
      prev?.key === key && prev.value.status === "ready"
        ? { key, value: { ...prev.value, refreshing: true } }
        : prev,
    );
    await run(key);
  }, [key, run]);

  const update = useCallback(
    (change: (data: T) => T) =>
      setHeld((prev) =>
        prev?.key === key && prev.value.status === "ready"
          ? { key, value: { ...prev.value, data: change(prev.value.data) } }
          : prev,
      ),
    [key],
  );

  const state: Loadable<T> =
    held !== null && held.key === key ? held.value : { status: "loading" };
  return { state, reload, update };
}
