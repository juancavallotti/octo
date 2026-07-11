import { describe, it, expect } from "vitest";
import {
  fileMetaFor,
  parseEditorMeta,
  serializeEditorMeta,
  withFileMeta,
} from "./parse";
import { emptyMeta } from "./types";

const SAMPLE = {
  version: 1,
  resources: {
    "orders.yaml": {
      flows: {
        orders: {
          inputs: [
            { id: "i1", name: "happy path", data: '{"orderId":42}', vars: '{"tier":"gold"}' },
          ],
        },
      },
    },
  },
};

describe("parseEditorMeta", () => {
  it("reads a well-formed file", () => {
    const meta = parseEditorMeta(JSON.stringify(SAMPLE));
    const input = meta.resources["orders.yaml"].flows.orders.inputs[0];

    expect(input).toEqual({
      id: "i1",
      name: "happy path",
      data: '{"orderId":42}',
      vars: '{"tier":"gold"}',
    });
  });

  it("round-trips through serialize", () => {
    const once = parseEditorMeta(JSON.stringify(SAMPLE));
    expect(parseEditorMeta(serializeEditorMeta(once))).toEqual(once);
  });

  // The file is a convenience, not a source of truth: anything unreadable degrades to
  // "no saved inputs" rather than throwing and taking the editor down with it.
  describe("leniency", () => {
    it.each([
      ["empty content", ""],
      ["whitespace", "   \n "],
      ["malformed JSON", "{not json"],
      ["a JSON scalar", '"nope"'],
      ["an array", "[]"],
      ["an object with no resources", "{}"],
      ["resources of the wrong type", '{"resources": 3}'],
    ])("degrades %s to an empty meta", (_what, content) => {
      expect(parseEditorMeta(content)).toEqual(emptyMeta());
    });

    it("drops an input with no id or name rather than the whole file", () => {
      const meta = parseEditorMeta(
        JSON.stringify({
          resources: {
            "a.yaml": {
              flows: {
                f: {
                  inputs: [
                    { name: "no id" },
                    { id: "i2", name: "fine" },
                    { id: "i3" },
                    "not even an object",
                  ],
                },
              },
            },
          },
        }),
      );

      expect(meta.resources["a.yaml"].flows.f.inputs).toEqual([{ id: "i2", name: "fine" }]);
    });

    it("tolerates a flow entry that is not an object", () => {
      const meta = parseEditorMeta(
        JSON.stringify({ resources: { "a.yaml": { flows: { f: 7 } } } }),
      );
      expect(meta.resources["a.yaml"].flows.f.inputs).toEqual([]);
    });
  });

  // Mocks are not implemented, but a file written by a newer editor must survive a
  // round-trip through this one rather than being silently stripped.
  it("preserves the reserved mocks list", () => {
    const withMocks = {
      resources: {
        "a.yaml": {
          flows: { f: { inputs: [], mocks: [{ block: "f.charge", result: "{}" }] } },
        },
      },
    };
    const meta = parseEditorMeta(JSON.stringify(withMocks));
    expect(meta.resources["a.yaml"].flows.f.mocks).toEqual([
      { block: "f.charge", result: "{}" },
    ]);
    expect(parseEditorMeta(serializeEditorMeta(meta))).toEqual(meta);
  });
});

describe("serializeEditorMeta", () => {
  // The file lives beside the flows and people will commit it, so it has to diff
  // cleanly: stable key order and a trailing newline.
  it("sorts keys and ends with a newline", () => {
    const meta = parseEditorMeta(
      JSON.stringify({
        resources: {
          "z.yaml": { flows: { b: { inputs: [] }, a: { inputs: [] } } },
          "a.yaml": { flows: {} },
        },
      }),
    );
    const out = serializeEditorMeta(meta);

    expect(out.indexOf("a.yaml")).toBeLessThan(out.indexOf("z.yaml"));
    expect(out.indexOf('"a"')).toBeLessThan(out.indexOf('"b"'));
    expect(out.endsWith("\n")).toBe(true);
  });
});

describe("fileMetaFor / withFileMeta", () => {
  it("returns an empty entry for an unknown document", () => {
    expect(fileMetaFor(emptyMeta(), "nope.yaml")).toEqual({ flows: {} });
  });

  it("stores an entry without disturbing the others", () => {
    const base = parseEditorMeta(JSON.stringify(SAMPLE));
    const next = withFileMeta(base, "other.yaml", { flows: {} });

    expect(next.resources["other.yaml"]).toEqual({ flows: {} });
    expect(next.resources["orders.yaml"]).toEqual(base.resources["orders.yaml"]);
    expect(base.resources["other.yaml"]).toBeUndefined(); // the original is untouched
  });
});
