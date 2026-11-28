import { describe, expect, it } from "vitest";
import type {
  CreateResourceInput,
  ResourceStore,
  StoredResource,
} from "../../providers/ResourceStoreProvider";
import {
  mintedNames,
  parseDays,
  parseHosts,
  storeMintedPair,
  withMintedPair,
} from "./tlsServer";

describe("parseHosts", () => {
  it("splits and trims, dropping blanks", () => {
    expect(parseHosts(" localhost, 127.0.0.1 ,, ")).toEqual(["localhost", "127.0.0.1"]);
  });
});

describe("mintedNames", () => {
  it("names the pair after the common name", () => {
    expect(mintedNames("localhost", [])).toEqual({
      cert: "tls/localhost.crt",
      key: "tls/localhost.key",
    });
  });

  it("numbers past a pair already stored", () => {
    expect(mintedNames("localhost", ["tls/localhost.crt"]).cert).toBe("tls/localhost-2.crt");
  });

  it("replaces characters a resource path should not carry", () => {
    expect(mintedNames("*.example.com", []).cert).toBe("tls/_.example.com.crt");
  });
});

describe("withMintedPair", () => {
  it("replaces every other certificate source and keeps the rest", () => {
    expect(
      withMintedPair(
        { port: 8443, cert: "pem", key: "pem", certFile: "a", keyFile: "b" },
        { cert: "tls/x.crt", key: "tls/x.key" },
      ),
    ).toEqual({ port: 8443, certResource: "tls/x.crt", keyResource: "tls/x.key" });
  });
});

describe("parseDays", () => {
  it("accepts a whole number of days", () => {
    expect(parseDays(" 30 ")).toBe(30);
  });

  it("rejects zero, fractions and blanks instead of defaulting", () => {
    expect(parseDays("0")).toBeNull();
    expect(parseDays("1.5")).toBeNull();
    expect(parseDays("")).toBeNull();
    expect(parseDays("-3")).toBeNull();
  });
});

/** An in-memory store that refuses to overwrite, like the disk store. */
function memoryStore(opts: { failKey?: boolean; beforeCreate?: (name: string, rows: StoredResource[]) => void } = {}) {
  const rows: StoredResource[] = [];
  const store: ResourceStore = {
    list: async () => rows.map((r) => ({ ...r })),
    create: async (input: CreateResourceInput) => {
      opts.beforeCreate?.(input.name, rows);
      if (opts.failKey && input.name.endsWith(".key")) throw new Error("disk full");
      if (rows.some((r) => r.name === input.name)) throw new Error("exists");
      const row = { id: input.name, ...input };
      rows.push(row);
      return row;
    },
    update: async () => {
      throw new Error("unused");
    },
    move: async () => {
      throw new Error("unused");
    },
    remove: async (id) => {
      const i = rows.findIndex((r) => r.id === id);
      if (i >= 0) rows.splice(i, 1);
    },
  };
  return { store, rows };
}

const PAIR = { cert: "CERT", key: "KEY" };

describe("storeMintedPair", () => {
  it("skips names the project already declares", async () => {
    const { store } = memoryStore();
    const out = await storeMintedPair(store, "localhost", PAIR, () => ["tls/localhost.crt"]);
    expect(out.names.cert).toBe("tls/localhost-2.crt");
  });

  it("moves on to the next suffix when another mint takes the name first", async () => {
    let raced = false;
    const { store, rows } = memoryStore({
      beforeCreate: (name, current) => {
        if (!raced && name === "tls/localhost.crt") {
          raced = true;
          current.push({ id: name, name, kind: "template", content: "OTHER" });
        }
      },
    });
    const out = await storeMintedPair(store, "localhost", PAIR, () => []);
    expect(out.names).toEqual({ cert: "tls/localhost-2.crt", key: "tls/localhost-2.key" });
    expect(rows.find((r) => r.name === "tls/localhost.crt")?.content).toBe("OTHER");
  });

  it("removes the certificate when the key cannot be stored", async () => {
    const { store, rows } = memoryStore({ failKey: true });
    await expect(storeMintedPair(store, "localhost", PAIR, () => [])).rejects.toThrow("disk full");
    expect(rows).toEqual([]);
  });
});
