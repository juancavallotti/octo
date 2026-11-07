import { describe, expect, it } from "vitest";
import { renderHook } from "@testing-library/react";
import { heldBack, useRunEnvironment } from "./useRunEnvironment";

describe("useRunEnvironment", () => {
  it("keeps the environment a run started in until it ends", () => {
    const { result, rerender } = renderHook(
      ({ busy, environment }) => useRunEnvironment(busy, environment),
      {
        initialProps: { busy: false, environment: "" },
      },
    );
    rerender({ busy: true, environment: "" });
    rerender({ busy: true, environment: "staging" });
    expect(result.current).toBe("");
    rerender({ busy: false, environment: "staging" });
    expect(result.current).toBe("staging");
  });
});

describe("heldBack", () => {
  it("holds files, and a message for a run begun in another environment, while he works", () => {
    expect(heldBack(true, true, "", "")).toMatch(/files/);
    expect(heldBack(true, false, "", "staging")).toMatch(/environment he started in/);
  });
  it("lets anything through once he is done, and a plain steer in the same environment", () => {
    expect(heldBack(false, true, "", "staging")).toBeNull();
    expect(heldBack(true, false, "staging", "staging")).toBeNull();
  });
});
