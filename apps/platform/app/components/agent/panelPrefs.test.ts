/**
 * The remembered panel layout. Mostly this is about what happens when storage
 * cannot be read: absent, blocked, or holding nonsense all have to yield a usable
 * panel, never an exception on the way through a layout every signed-in page
 * renders.
 *
 * Note that this test environment is itself a storage-less one — jsdom's
 * localStorage is shadowed here — so a working store has to be installed for the
 * round-trip cases, and the absent case is simply the default.
 */

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  clampWidth,
  DEFAULT_WIDTH,
  MAX_WIDTH,
  MIN_WIDTH,
  readDocked,
  readWidth,
  writeDocked,
  writeWidth,
} from "./panelPrefs";

/** Install a Map-backed localStorage on the window for one test. */
function withStorage(): Map<string, string> {
  const store = new Map<string, string>();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
    },
  });
  return store;
}

/** A localStorage that throws on access, as a sandboxed frame's does. */
function withBlockedStorage(): void {
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    get() {
      throw new Error("blocked");
    },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(window, "localStorage");
});

describe("docked", () => {
  it("floats by default", () => {
    withStorage();
    expect(readDocked()).toBe(false);
  });

  it("round-trips both ways", () => {
    withStorage();
    writeDocked(true);
    expect(readDocked()).toBe(true);
    writeDocked(false);
    expect(readDocked()).toBe(false);
  });

  it("floats where there is no storage at all", () => {
    expect(window.localStorage).toBeUndefined();
    expect(readDocked()).toBe(false);
    expect(() => writeDocked(true)).not.toThrow();
  });

  it("floats when storage is blocked, and writing does not throw", () => {
    withBlockedStorage();
    expect(readDocked()).toBe(false);
    expect(() => writeDocked(true)).not.toThrow();
  });

  it("floats when there is no window", () => {
    vi.stubGlobal("window", undefined);
    expect(readDocked()).toBe(false);
    expect(() => writeDocked(true)).not.toThrow();
  });
});

describe("width", () => {
  it("defaults when nothing is stored", () => {
    withStorage();
    expect(readWidth()).toBe(DEFAULT_WIDTH);
  });

  it("round-trips a width in range", () => {
    withStorage();
    writeWidth(600);
    expect(readWidth()).toBe(600);
  });

  it("clamps to the draggable range in both directions", () => {
    expect(clampWidth(10)).toBe(MIN_WIDTH);
    expect(clampWidth(9000)).toBe(MAX_WIDTH);
    const store = withStorage();
    writeWidth(9000);
    expect(store.get("octo.agent.width")).toBe(String(MAX_WIDTH));
    expect(readWidth()).toBe(MAX_WIDTH);
  });

  it("clamps a stored value that is out of range", () => {
    withStorage().set("octo.agent.width", "20");
    expect(readWidth()).toBe(MIN_WIDTH);
  });

  it("defaults on a malformed stored value", () => {
    withStorage().set("octo.agent.width", "wide-ish");
    expect(readWidth()).toBe(DEFAULT_WIDTH);
    expect(clampWidth(Number.NaN)).toBe(DEFAULT_WIDTH);
  });

  it("defaults when storage is blocked, and writing does not throw", () => {
    withBlockedStorage();
    expect(readWidth()).toBe(DEFAULT_WIDTH);
    expect(() => writeWidth(600)).not.toThrow();
  });
});
