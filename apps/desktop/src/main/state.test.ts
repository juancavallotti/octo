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
