import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

/**
 * The deployments page's rollout flow, at the point where it differs from the
 * integrations panel's: it holds no integration's tags, so opening the dialog is
 * what fetches them. That makes the dialog's contents the result of a request in
 * flight, and a page listing every deployment is exactly where a second one gets
 * opened before the first has landed.
 */

const listSnapshots = vi.fn();
const createSnapshot = vi.fn();
const rolloutDeployment = vi.fn();

vi.mock("@/app/model/orchestrator", () => ({
  listSnapshots: (id: string) => listSnapshots(id),
  createSnapshot: (id: string, tag: string) => createSnapshot(id, tag),
  rolloutDeployment: (
    id: string,
    snapshotId: string,
    env: unknown,
    tracing: boolean,
  ) => rolloutDeployment(id, snapshotId, env, tracing),
}));

import { useRollout } from "./useRollout";
import type { DeployedTile } from "@/app/(session)/platform/DashboardTiles";

const tile = (id: string, integrationId: string) =>
  ({ id, integrationId, integrationName: integrationId }) as DeployedTile;

/** A promise plus the handle to settle it, so a test controls the ordering. */
function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

afterEach(() => vi.clearAllMocks());

describe("useRollout", () => {
  it("loads the target's tags when the dialog opens", async () => {
    listSnapshots.mockResolvedValue([{ id: "s-1", tag: "v1" }]);
    const { result } = renderHook(() => useRollout(() => {}));

    act(() => result.current.open(tile("dep-1", "int-1")));

    expect(listSnapshots).toHaveBeenCalledWith("int-1");
    await waitFor(() => expect(result.current.snapshots).toHaveLength(1));
    expect(result.current.target?.id).toBe("dep-1");
  });

  // The finding this exists for: two deployments opened in a row, the first
  // integration's tags arriving last. Landing them would put int-1's versions in
  // int-2's picker, and rolling out to the wrong one would succeed.
  it("ignores tags that arrive after another deployment was opened", async () => {
    const first = deferred<unknown[]>();
    const second = deferred<unknown[]>();
    listSnapshots
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);

    const { result } = renderHook(() => useRollout(() => {}));
    act(() => result.current.open(tile("dep-1", "int-1")));
    act(() => result.current.open(tile("dep-2", "int-2")));

    await act(async () => {
      second.resolve([{ id: "s-2", tag: "int-2-tag" }]);
      first.resolve([{ id: "s-1", tag: "int-1-tag" }]);
      await first.promise;
    });

    expect(result.current.target?.id).toBe("dep-2");
    expect(result.current.snapshots).toEqual([{ id: "s-2", tag: "int-2-tag" }]);
  });

  it("ignores a failure from a request the dialog has moved on from", async () => {
    const first = deferred<unknown[]>();
    listSnapshots
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce([{ id: "s-2", tag: "int-2-tag" }]);

    const { result } = renderHook(() => useRollout(() => {}));
    act(() => result.current.open(tile("dep-1", "int-1")));
    act(() => result.current.open(tile("dep-2", "int-2")));

    await act(async () => {
      first.reject(new Error("int-1 is gone"));
      await first.promise.catch(() => {});
    });

    expect(result.current.error).toBeNull();
  });

  it("drops a pending request when the dialog is closed", async () => {
    const first = deferred<unknown[]>();
    listSnapshots.mockReturnValueOnce(first.promise);

    const { result } = renderHook(() => useRollout(() => {}));
    act(() => result.current.open(tile("dep-1", "int-1")));
    act(() => result.current.close());

    await act(async () => {
      first.resolve([{ id: "s-1", tag: "int-1-tag" }]);
      await first.promise;
    });

    expect(result.current.target).toBeNull();
    expect(result.current.snapshots).toEqual([]);
  });

  it("cuts a new tag before rolling over to it, then refreshes", async () => {
    listSnapshots.mockResolvedValue([]);
    createSnapshot.mockResolvedValue({ id: "s-new" });
    rolloutDeployment.mockResolvedValue({});
    const onDone = vi.fn();

    const { result } = renderHook(() => useRollout(onDone));
    act(() => result.current.open(tile("dep-1", "int-1")));

    await act(async () => {
      await result.current.submit({
        deploymentId: "dep-1",
        newTag: "v2",
        env: {},
        tracing: true,
      });
    });

    expect(createSnapshot).toHaveBeenCalledWith("int-1", "v2");
    expect(rolloutDeployment).toHaveBeenCalledWith("dep-1", "s-new", {}, true);
    expect(onDone).toHaveBeenCalled();
    expect(result.current.target).toBeNull();
  });

  // A failed rollout keeps the dialog open with its message: the operator has
  // just edited the env, and closing would take that away along with the error.
  it("keeps the dialog open when the rollout fails", async () => {
    listSnapshots.mockResolvedValue([{ id: "s-1", tag: "v1" }]);
    rolloutDeployment.mockRejectedValue(new Error("cluster said no"));

    const { result } = renderHook(() => useRollout(() => {}));
    act(() => result.current.open(tile("dep-1", "int-1")));

    await act(async () => {
      await result.current.submit({
        deploymentId: "dep-1",
        snapshotId: "s-1",
        env: {},
        tracing: false,
      });
    });

    expect(result.current.error).toBe("cluster said no");
    expect(result.current.target?.id).toBe("dep-1");
  });
});
