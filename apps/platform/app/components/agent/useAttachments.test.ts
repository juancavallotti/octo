/**
 * The rule worth pinning here is the one that is easy to get wrong: the limits
 * are about the *message*, so a drop of several files is checked against what
 * the drop has already taken as well as what was already there.
 */

import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { MAX_FILES } from "./attachments";
import { useAttachments } from "./useAttachments";

const ACCEPTS = ["image/png"];

function png(name: string, size = 10): File {
  return new File(["x".repeat(size)], name, { type: "image/png" });
}

describe("useAttachments", () => {
  it("keeps the files that fit", async () => {
    const { result } = renderHook(() => useAttachments(ACCEPTS));

    act(() => result.current.add([png("a.png"), png("b.png")]));

    await waitFor(() => expect(result.current.files).toHaveLength(2));
    expect(result.current.rejected).toBeNull();
  });

  /**
   * The bug this prevents: a loop that checked the React state would let every
   * file in one drop pass the same "is there room" test, because the state has
   * not been updated yet.
   */
  it("counts what the same drop has already taken", async () => {
    const { result } = renderHook(() => useAttachments(ACCEPTS));

    act(() => result.current.add(Array.from({ length: MAX_FILES + 3 }, (_, i) => png(`${i}.png`))));

    await waitFor(() => expect(result.current.files).toHaveLength(MAX_FILES));
    expect(result.current.rejected).toMatch(new RegExp(`${MAX_FILES} files`));
  });

  // The first reason, not the last: a person fixes one thing at a time.
  it("reports the first refusal and keeps the rest", async () => {
    const { result } = renderHook(() => useAttachments(ACCEPTS));

    act(() =>
      result.current.add([
        new File(["x"], "note.mp3", { type: "audio/mpeg" }),
        png("shot.png"),
      ]),
    );

    await waitFor(() => expect(result.current.files).toHaveLength(1));
    expect(result.current.rejected).toMatch(/note\.mp3/);
  });

  it("removes one by id, and clears everything when the message is sent", async () => {
    const { result } = renderHook(() => useAttachments(ACCEPTS));
    act(() => result.current.add([png("a.png"), png("b.png")]));
    await waitFor(() => expect(result.current.files).toHaveLength(2));

    const first = result.current.files[0].id;
    act(() => result.current.remove(first));
    await waitFor(() => expect(result.current.files).toHaveLength(1));

    act(() => result.current.clear());
    await waitFor(() => expect(result.current.files).toHaveLength(0));
    expect(result.current.rejected).toBeNull();
  });

  it("takes nothing when the model reads no files", async () => {
    const { result } = renderHook(() => useAttachments([]));

    act(() => result.current.add([png("shot.png")]));

    await waitFor(() => expect(result.current.rejected).toMatch(/does not accept/));
    expect(result.current.files).toHaveLength(0);
  });
});
