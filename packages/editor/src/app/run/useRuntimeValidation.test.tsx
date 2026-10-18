import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { EditorStateProvider } from "../state/editorState";
import { blankDocument } from "../model/document";
import type { RunTransport, RuntimeIssue } from "./transport";
import { useRuntimeValidation } from "./useRuntimeValidation";

const wrapper = ({ children }: { children: ReactNode }) => <EditorStateProvider>{children}</EditorStateProvider>;
const ok = { ok: true, issues: [] };
const target = {};

function transport(found: RuntimeIssue[]): RunTransport {
  return { validate: vi.fn(async () => found) } as unknown as RunTransport;
}

describe("useRuntimeValidation", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("adds what the runtime finds once the editor's checks pass", async () => {
    const t = transport([{ where: "orders.flow-ref", message: 'flow: flow "audit" is not defined' }]);
    const { result } = renderHook(() => useRuntimeValidation(blankDocument(), ok, t, target), { wrapper });
    expect(result.current).toBe(ok);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(t.validate).toHaveBeenCalledWith({ files: [{ path: "octo.yaml", content: expect.any(String) }] });
    expect(result.current).toEqual({
      ok: false,
      issues: [{ message: 'orders.flow-ref: flow: flow "audit" is not defined', severity: "error", blockId: undefined }],
    });
  });

  it("does not ask the runtime while the editor already has issues", async () => {
    const t = transport([]);
    const failing = { ok: false, issues: [{ message: "x", severity: "error" as const }] };
    renderHook(() => useRuntimeValidation(blankDocument(), failing, t, target), { wrapper });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(t.validate).not.toHaveBeenCalled();
  });
});
