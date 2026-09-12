import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { EMPTY, MAX_RECENTS, existing, read, remember, stateFile, write } from "./state";

/**
 * Two things are worth pinning here. First, that a damaged state file degrades to
 * "no memory" rather than to a crash — this file is a convenience, and refusing to
 * launch over it would be wildly out of proportion. Second, the recents ordering,
 * which is the kind of small logic that is easy to get subtly wrong (a duplicate
 * that does not move to the top, a cap applied before the dedupe).
 */

const dirs: string[] = [];

function scratch(): string {
  const dir = mkdtempSync(path.join(tmpdir(), "octo-desktop-state-"));
  dirs.push(dir);
  return dir;
}

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe("read", () => {
  it("returns the empty state when there is no file", () => {
    expect(read(scratch())).toEqual(EMPTY);
  });

  it("round-trips what write stored", () => {
    const dir = scratch();
    const state = { lastVault: "/a", recents: [{ path: "/a", openedAt: 7 }], port: 9001 };
    write(dir, state);
    expect(read(dir)).toMatchObject(state);
  });

  for (const [name, body] of [
    ["truncated json", '{"recents": ['],
    ["json of the wrong type", '"a string"'],
    ["null", "null"],
    ["empty", ""],
  ] as const) {
    it(`degrades to the empty state on ${name}`, () => {
      const dir = scratch();
      writeFileSync(stateFile(dir), body);
      expect(read(dir)).toEqual(EMPTY);
    });
  }

  it("drops recents entries of the wrong shape rather than trusting them", () => {
    const dir = scratch();
    writeFileSync(
      stateFile(dir),
      JSON.stringify({ recents: [{ path: "/good", openedAt: 1 }, { path: 5 }, null, {}] }),
    );
    expect(read(dir).recents).toEqual([{ path: "/good", openedAt: 1 }]);
  });
});

describe("read: window geometry", () => {
  const stored = (window: unknown) => {
    const dir = scratch();
    writeFileSync(stateFile(dir), JSON.stringify({ recents: [], window }));
    return read(dir).window;
  };

  it("keeps a usable rectangle", () => {
    expect(stored({ x: 10, y: 20, width: 1440, height: 900 })).toEqual({
      x: 10, y: 20, width: 1440, height: 900,
    });
    // x/y are optional: a window that was never moved has a size and no position.
    expect(stored({ width: 800, height: 600 })).toEqual({ width: 800, height: 600 });
  });

  it("drops anything BrowserWindow would choke on", () => {
    // This file survives upgrades and can be hand-edited, so a bad value here
    // would otherwise be a launch failure caused by a remembered convenience.
    for (const bad of [
      { width: "1440", height: 900 },
      { width: 1440 },
      { width: 0, height: 900 },
      { width: -100, height: 900 },
      { width: 1440, height: 900, x: "left" },
      "not an object",
      42,
      null,
    ]) {
      expect(stored(bad), JSON.stringify(bad)).toBeUndefined();
    }
  });
});

describe("write", () => {
  it("leaves no temp file behind", () => {
    const dir = scratch();
    write(dir, EMPTY);
    expect(() => read(dir)).not.toThrow();
    expect(existing([{ path: `${stateFile(dir)}.tmp`, openedAt: 0 }])).toEqual([]);
  });
});

describe("remember", () => {
  it("puts the newest folder first", () => {
    const after = remember(remember(EMPTY, "/a", 1), "/b", 2);
    expect(after.recents.map((v) => v.path)).toEqual(["/b", "/a"]);
    expect(after.lastVault).toBe("/b");
  });

  it("moves a folder you reopen back to the top instead of duplicating it", () => {
    let state = remember(remember(remember(EMPTY, "/a", 1), "/b", 2), "/c", 3);
    state = remember(state, "/a", 4);
    expect(state.recents.map((v) => v.path)).toEqual(["/a", "/c", "/b"]);
  });

  it("caps the list, keeping the most recent", () => {
    let state = EMPTY;
    for (let i = 0; i < MAX_RECENTS + 5; i++) state = remember(state, `/v${i}`, i);
    expect(state.recents).toHaveLength(MAX_RECENTS);
    expect(state.recents[0].path).toBe(`/v${MAX_RECENTS + 4}`);
  });

  it("dedupes before capping, so reopening an old folder cannot shrink the list", () => {
    let state = EMPTY;
    for (let i = 0; i < MAX_RECENTS; i++) state = remember(state, `/v${i}`, i);
    state = remember(state, "/v0", 99);
    expect(state.recents).toHaveLength(MAX_RECENTS);
  });
});

describe("existing", () => {
  it("hides folders that are no longer there", () => {
    const dir = scratch();
    const live = path.join(dir, "live");
    mkdirSync(live);
    const recents = [
      { path: live, openedAt: 2 },
      { path: path.join(dir, "gone"), openedAt: 1 },
    ];
    expect(existing(recents).map((v) => v.path)).toEqual([live]);
  });
});

/**
 * Settings are validated on read for the same reason everything else here is: the
 * file is user-editable, and a runtime path of the wrong type reaches spawn() as a
 * non-string and fails the launch. A hand-edited mistake should cost the setting.
 */
describe("settings", () => {
  const stored = (settings: unknown) => {
    const dir = scratch();
    writeFileSync(stateFile(dir), JSON.stringify({ recents: [], settings }), "utf8");
    return read(dir).settings;
  };

  it("round-trips what was chosen", () => {
    expect(stored({ runtime: { octo: "/opt/octo" }, autoUpdateCheck: false })).toEqual({
      runtime: { octo: "/opt/octo" },
      autoUpdateCheck: false,
    });
  });

  it("drops a path that is not a string", () => {
    expect(stored({ runtime: { octo: 7, dolphin: "/opt/dolphin" } })).toEqual({
      runtime: { dolphin: "/opt/dolphin" },
    });
  });

  it("drops an empty path rather than storing a meaningless override", () => {
    expect(stored({ runtime: { octo: "" } })).toEqual({});
  });

  it("drops a runtime that is not an object", () => {
    expect(stored({ runtime: "octo" })).toEqual({});
  });

  it("drops a non-boolean update flag", () => {
    expect(stored({ autoUpdateCheck: "yes" })).toEqual({});
  });

  it("is absent when there are no settings at all", () => {
    expect(stored(undefined)).toBeUndefined();
    expect(read(scratch()).settings).toBeUndefined();
  });
});
