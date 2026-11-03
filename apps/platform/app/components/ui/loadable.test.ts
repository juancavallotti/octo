import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useLoadable } from "./loadable";

/**
 * The loading states a page shows, pinned: a first load is loading, a reload is
 * not, and a new subject starts over rather than showing the last one's data.
 */

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

it("loads, then is ready with the data", async () => {
  const { result } = renderHook(() => useLoadable("k", async () => [1, 2]));
  expect(result.current.state).toEqual({ status: "loading" });
  await waitFor(() =>
    expect(result.current.state).toEqual({
      status: "ready",
      data: [1, 2],
      refreshing: false,
      error: null,
    }),
  );
});

it("fetches nothing and stays loading until there is a key", () => {
  const load = vi.fn(async () => 1);
  const { result } = renderHook(() => useLoadable(null, load));
  expect(result.current.state.status).toBe("loading");
  expect(load).not.toHaveBeenCalled();
});

// The point of the pattern: a reload is not a first load, so the view keeps what
// it has instead of going back to a skeleton.
it("keeps the data on screen while it reloads", async () => {
  let next = deferred<number>();
  const { result } = renderHook(() => useLoadable("k", () => next.promise));
  next.resolve(1);
  await waitFor(() => expect(result.current.state.status).toBe("ready"));

  next = deferred<number>();
  let reloading!: Promise<void>;
  act(() => {
    reloading = result.current.reload();
  });
  expect(result.current.state).toMatchObject({ status: "ready", data: 1, refreshing: true });

  await act(async () => {
    next.resolve(2);
    await reloading;
  });
  expect(result.current.state).toMatchObject({ status: "ready", data: 2, refreshing: false });
});

it("keeps the data and reports why when a reload fails", async () => {
  let next = deferred<number>();
  const { result } = renderHook(() => useLoadable("k", () => next.promise));
  next.resolve(1);
  await waitFor(() => expect(result.current.state.status).toBe("ready"));

  next = deferred<number>();
  await act(async () => {
    const reloading = result.current.reload();
    next.reject(new Error("gone"));
    await reloading;
  });
  expect(result.current.state).toMatchObject({ status: "ready", data: 1, error: "gone" });
});

it("is failed when the first load fails", async () => {
  const { result } = renderHook(() =>
    useLoadable("k", async () => {
      throw new Error("no orchestrator");
    }),
  );
  await waitFor(() =>
    expect(result.current.state).toEqual({ status: "failed", error: "no orchestrator" }),
  );
});

// Showing the previous integration's data under the next one's name is worse
// than a skeleton.
it("starts over from loading when the key changes", async () => {
  const { result, rerender } = renderHook(({ k }) => useLoadable(k, async () => k), {
    initialProps: { k: "a" },
  });
  await waitFor(() => expect(result.current.state).toMatchObject({ data: "a" }));
  rerender({ k: "b" });
  expect(result.current.state.status).toBe("loading");
  await waitFor(() => expect(result.current.state).toMatchObject({ data: "b" }));
});

it("applies an optimistic edit to the data in place", async () => {
  const { result } = renderHook(() => useLoadable("k", async () => [1]));
  await waitFor(() => expect(result.current.state.status).toBe("ready"));
  act(() => result.current.update((d) => [...d, 2]));
  expect(result.current.state).toMatchObject({ data: [1, 2] });
});
