import { describe, expect, it } from "vitest";

import { fromFrozen, fromLive, guessKind } from "./resources";

const TS = "2026-08-09T10:00:00Z";

describe("guessKind", () => {
  it("treats the .env convention as env and everything else as a template", () => {
    expect(guessKind(".env")).toBe("env");
    expect(guessKind(".env.local")).toBe("env");
    expect(guessKind(".environment")).toBe("env"); // the prefix rule, as the loader has it
    expect(guessKind("prompt.md")).toBe("template");
    expect(guessKind("greeting.tmpl")).toBe("template");
    expect(guessKind("")).toBe("template");
  });

  // Resource names carry their relative path, so the rule has to look at the file
  // rather than the whole name — a template inside a directory called `.env` is
  // still a template, and an `.env` inside any directory is still env.
  it("decides on the last path segment", () => {
    expect(guessKind("config/.env")).toBe("env");
    expect(guessKind("a/b/.env.production")).toBe("env");
    expect(guessKind(".env/notes.md")).toBe("template");
  });
});

describe("display shapes", () => {
  it("keeps a live resource's id, which is what makes it deletable", () => {
    expect(fromLive({ id: "r1", integrationId: "i1", kind: "env", name: ".env", content: "", createdAt: TS, lastUpdated: TS })).toEqual({
      key: "r1",
      id: "r1",
      kind: "env",
      name: ".env",
    });
  });

  // A frozen resource belongs to its snapshot and has no id of its own. The
  // absent id is precisely what the list reads to hide the delete control, so it
  // must stay absent rather than become an empty string.
  it("gives a frozen resource no id", () => {
    const row = fromFrozen({ kind: "template", name: "prompt.md", createdAt: "" });
    expect(row.id).toBeUndefined();
    expect(row.key).toBe("template:prompt.md");
  });
});
