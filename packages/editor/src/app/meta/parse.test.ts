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

  describe("mocks", () => {
    const file = (flow: unknown) => JSON.stringify({ resources: { "a.yaml": { flows: { f: flow } } } });
    const mocksOf = (json: string) => parseEditorMeta(json).resources["a.yaml"].flows.f.mocks;

    it("round-trips a mock through parse and serialize", () => {
      const meta = parseEditorMeta(
        file({
          inputs: [],
          mocks: [
            {
              address: "f.charge",
              enabled: true,
              cases: [{ when: "body.amount > 100", error: "card declined" }],
              default: { body: '{"ok":true}' },
            },
          ],
        }),
      );
      expect(meta.resources["a.yaml"].flows.f.mocks).toEqual([
        {
          address: "f.charge",
          enabled: true,
          cases: [{ when: "body.amount > 100", error: "card declined" }],
          default: { body: '{"ok":true}' },
        },
      ]);
      expect(parseEditorMeta(serializeEditorMeta(meta))).toEqual(meta);
    });

    // A mock is saved because the user wants it; the toggle is how they say otherwise.
    it("treats a mock written without `enabled` as enabled", () => {
      expect(mocksOf(file({ inputs: [], mocks: [{ address: "f.charge", cases: [] }] }))).toEqual([
        { address: "f.charge", enabled: true, cases: [] },
      ]);
    });

    it("keeps a disabled mock rather than dropping it", () => {
      const mocks = mocksOf(
        file({ inputs: [], mocks: [{ address: "f.charge", enabled: false, cases: [] }] }),
      );
      expect(mocks?.[0].enabled).toBe(false);
    });

    // The address is the only handle on a mock; without one it stands in for nothing.
    it("drops a mock with no address", () => {
      expect(mocksOf(file({ inputs: [], mocks: [{ cases: [] }, { address: "f.a", cases: [] }] })))
        .toEqual([{ address: "f.a", enabled: true, cases: [] }]);
    });

    it("survives a hand-mangled entry without losing the rest of the file", () => {
      const meta = parseEditorMeta(file({ inputs: [{ id: "i1", name: "one" }], mocks: "nonsense" }));
      expect(meta.resources["a.yaml"].flows.f.inputs).toHaveLength(1);
      expect(meta.resources["a.yaml"].flows.f.mocks).toBeUndefined();
    });
  });

  describe("spies", () => {
    const file = (spies: unknown) =>
      JSON.stringify({ resources: { "a.yaml": { flows: { f: { inputs: [], spies } } } } });
    const spiesOf = (json: string) => parseEditorMeta(json).resources["a.yaml"].flows.f.spies;

    it("round-trips the spied addresses", () => {
      const meta = parseEditorMeta(file(["f.seed", "f.fanout[audit].log-it"]));
      expect(meta.resources["a.yaml"].flows.f.spies).toEqual(["f.seed", "f.fanout[audit].log-it"]);
      expect(parseEditorMeta(serializeEditorMeta(meta))).toEqual(meta);
    });

    // A spy is on or it is not, so the same address twice means nothing extra — and the
    // runtime rejects a duplicate outright ("spy %q is listed twice").
    it("de-duplicates, and drops anything that is not an address", () => {
      expect(spiesOf(file(["f.seed", "f.seed", "", 42, null]))).toEqual(["f.seed"]);
    });

    it("omits the key entirely when nothing is spied", () => {
      expect(spiesOf(file([]))).toBeUndefined();
    });
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
