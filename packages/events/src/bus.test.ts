import { describe, it, expect, vi } from "vitest";
import { publish, subscribe } from "./bus";
import type { OctoEvent } from "./types";

const event: OctoEvent = {
  type: "integration.updated",
  id: "abc",
  name: "Demo",
};

describe("event bus", () => {
  it("delivers a published event to every subscriber", () => {
    const a = vi.fn();
    const b = vi.fn();
    const offA = subscribe(a);
    const offB = subscribe(b);
    publish(event);
    expect(a).toHaveBeenCalledWith(event);
    expect(b).toHaveBeenCalledWith(event);
    offA();
    offB();
  });

  it("stops delivering after unsubscribe", () => {
    const fn = vi.fn();
    const off = subscribe(fn);
    off();
    publish(event);
    expect(fn).not.toHaveBeenCalled();
  });

  it("isolates a throwing subscriber from the others", () => {
    const bad = vi.fn(() => {
      throw new Error("boom");
    });
    const good = vi.fn();
    const offBad = subscribe(bad);
    const offGood = subscribe(good);
    expect(() => publish(event)).not.toThrow();
    expect(good).toHaveBeenCalledWith(event);
    offBad();
    offGood();
  });
});
