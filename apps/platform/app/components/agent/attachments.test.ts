/**
 * The limits a message's files are held to, which are about the *message* rather
 * than the file: five that each fit can still be four too many.
 */

import { describe, expect, it } from "vitest";

import {
  ACCEPTED,
  MAX_FILES,
  MAX_FILE_BYTES,
  acceptAttribute,
  acceptable,
  humanSize,
  sendable,
  readAttachment,
  wireAttachments,
  type Attachment,
} from "./attachments";
import { acceptedFor } from "./media";

const ACCEPTS = ["image/png", "application/pdf"];

function file(name: string, type: string, size = 10): File {
  const f = new File(["x".repeat(size)], name, { type });
  return f;
}

function held(size: number, id = "a"): Attachment {
  return { id, name: `${id}.png`, mimeType: "image/png", data: "", size };
}

describe("acceptable", () => {
  it("takes a file of an accepted type", () => {
    expect(acceptable(file("shot.png", "image/png"), [], ACCEPTS)).toBeNull();
  });

  it("refuses a type this model does not read, and says which", () => {
    const why = acceptable(file("note.mp3", "audio/mpeg"), [], ACCEPTS);
    expect(why).toMatch(/note\.mp3/);
    expect(why).toMatch(/audio\/mpeg/);
  });

  // A model that reads nothing is a different sentence from one that reads
  // something else: the thing to change is the model, not the file.
  it("refuses everything when the model reads no files", () => {
    expect(acceptable(file("shot.png", "image/png"), [], [])).toMatch(
      /does not accept attachments/,
    );
  });

  it("refuses a file over the per-file limit", () => {
    const big = file("huge.png", "image/png", MAX_FILE_BYTES + 1);
    expect(acceptable(big, [], ACCEPTS)).toMatch(/limit is/);
  });

  it("refuses one file too many", () => {
    const already = Array.from({ length: MAX_FILES }, (_, i) => held(1, `a${i}`));
    expect(acceptable(file("shot.png", "image/png"), already, ACCEPTS)).toMatch(
      new RegExp(`${MAX_FILES} files`),
    );
  });

  // The limit that only the message can break: each of these fits on its own.
  it("refuses a file that would put the message over the total", () => {
    const already = [held(MAX_FILE_BYTES), held(MAX_FILE_BYTES, "b")];
    expect(acceptable(file("third.png", "image/png", 1024), already, ACCEPTS)).toMatch(
      /more than/,
    );
  });
});

describe("readAttachment", () => {
  it("reads a file into the shape the request carries", async () => {
    const got = await readAttachment(file("shot.png", "image/png", 5));

    expect(got.name).toBe("shot.png");
    expect(got.mimeType).toBe("image/png");
    expect(got.size).toBe(5);
    expect(atob(got.data)).toBe("xxxxx");
    expect(got.id).toBeTruthy();
  });
});

describe("wireAttachments", () => {
  // The id and the decoded size are this window's bookkeeping; the runtime reads
  // neither.
  it("sends only what the runtime reads", () => {
    expect(wireAttachments([{ ...held(3), data: "cG5n", name: "shot.png" }])).toEqual([
      { name: "shot.png", mimeType: "image/png", data: "cG5n" },
    ]);
  });
});

describe("humanSize", () => {
  it.each([
    [512, "512 B"],
    [2048, "2 KB"],
    [3 * 1024 * 1024, "3.0 MB"],
  ])("renders %d as %s", (bytes, want) => {
    expect(humanSize(bytes)).toBe(want);
  });
});

/**
 * The runtime matches a family by prefix, so exact matching here would turn away
 * a .mov the model would have read. That drift is what cost video/quicktime.
 */
describe("sendable", () => {
  it("matches a family by prefix and anything else exactly", () => {
    const families = ["image/", "video/", "application/pdf"];
    for (const mime of ["image/png", "image/heif", "video/quicktime", "video/mp4"]) {
      expect(sendable(mime, families)).toBe(true);
    }
    expect(sendable("application/pdf", families)).toBe(true);
    expect(sendable("application/zip", families)).toBe(false);
    expect(sendable("audio/mpeg", families)).toBe(false);
  });

  it("does not let an exact entry match a whole family", () => {
    expect(sendable("image/png", ["image/png"])).toBe(true);
    expect(sendable("image/gif", ["image/png"])).toBe(false);
  });
});

describe("acceptAttribute", () => {
  // The table holds the runtime's spelling; a browser reads video/*. This is the
  // one place the two differ.
  it("writes a family the way a browser reads it", () => {
    expect(acceptAttribute(["video/", "application/pdf"])).toBe("video/*,application/pdf");
  });
});

describe("acceptedFor", () => {
  it("names what each shipped connector reads", () => {
    expect(acceptedFor("llm-anthropic")).toContain("application/pdf");
    // The one connector that reads a voice note or a clip — by family, as the
    // connector itself does, so a type nobody enumerated still gets through.
    expect(sendable("audio/mpeg", acceptedFor("llm-gemini"))).toBe(true);
    expect(sendable("video/quicktime", acceptedFor("llm-gemini"))).toBe(true);
    expect(sendable("audio/mpeg", acceptedFor("llm-openai"))).toBe(false);
  });

  /**
   * Offering a file that is then refused is worse than not offering one: the
   * refusal arrives after the upload, as a failed run.
   */
  it("offers nothing for a connector this build does not know", () => {
    expect(acceptedFor("llm-something-new")).toEqual([]);
    expect(acceptedFor(undefined)).toEqual([]);
  });

  // The fallback the composer uses when the status route cannot say, which must
  // be the intersection rather than the union for the same reason.
  it("keeps the fallback set inside every connector's own", () => {
    for (const connector of ["llm-anthropic", "llm-openai", "llm-gemini", "llm-openrouter"]) {
      for (const mime of ACCEPTED) {
        expect(sendable(mime, acceptedFor(connector))).toBe(true);
      }
    }
  });
});
