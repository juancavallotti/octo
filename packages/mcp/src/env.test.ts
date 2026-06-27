import { describe, expect, it } from "vitest";
import { parseEnv } from "./env";

describe("parseEnv", () => {
  it("accepts a map of valid names to string values", () => {
    expect(parseEnv({ API_KEY: "x", _Q2: "y" })).toEqual({ API_KEY: "x", _Q2: "y" });
  });

  it("accepts undefined-shaped empties", () => {
    expect(parseEnv({})).toEqual({});
  });

  it("rejects invalid names", () => {
    expect(parseEnv({ "1bad": "x" })).toBeNull();
    expect(parseEnv({ "has-dash": "x" })).toBeNull();
  });

  it("rejects non-string values", () => {
    expect(parseEnv({ OK: 1 })).toBeNull();
  });

  it("rejects non-objects", () => {
    expect(parseEnv(null)).toBeNull();
    expect(parseEnv([])).toBeNull();
    expect(parseEnv("x")).toBeNull();
  });
});
