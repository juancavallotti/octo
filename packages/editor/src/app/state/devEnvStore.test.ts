import { describe, expect, it } from "vitest";
import { parseDotEnv, serializeDotEnv } from "./devEnvStore";

describe("parseDotEnv", () => {
  it("parses KEY=VALUE lines, skipping blanks and comments", () => {
    const content = ["# a comment", "", "A=1", "export B=two", "C = 3 "].join("\n");
    expect(parseDotEnv(content)).toEqual({ A: "1", B: "two", C: "3" });
  });

  it("strips one pair of surrounding quotes and keeps inner content", () => {
    expect(parseDotEnv('A="he llo"\nB=\'x=y\'')).toEqual({ A: "he llo", B: "x=y" });
  });

  it("keeps everything after the first = and skips lines without one", () => {
    expect(parseDotEnv("URL=http://x/?a=b\nnonsense")).toEqual({
      URL: "http://x/?a=b",
    });
  });
});

describe("serializeDotEnv", () => {
  it("emits sorted KEY=VALUE lines with a trailing newline", () => {
    expect(serializeDotEnv({ B: "2", A: "1" })).toBe("A=1\nB=2\n");
  });

  it("returns an empty string for an empty map", () => {
    expect(serializeDotEnv({})).toBe("");
  });

  it("quotes values that would not survive trim/unquote", () => {
    expect(serializeDotEnv({ A: " padded " })).toBe('A=" padded "\n');
    expect(serializeDotEnv({ A: "#hash" })).toBe('A="#hash"\n');
  });

  it("round-trips through parseDotEnv", () => {
    const map = { TOKEN: "abc123", MSG: "hello world", TRAIL: "v " };
    expect(parseDotEnv(serializeDotEnv(map))).toEqual(map);
  });
});
