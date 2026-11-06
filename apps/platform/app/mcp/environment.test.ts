import { describe, expect, it } from "vitest";
import { mcpEnvironment } from "./environment";

describe("mcpEnvironment", () => {
  it("reads the header", () => {
    const req = new Request("https://p.example/mcp", { headers: { "X-Octo-Environment": "staging" } });
    expect(mcpEnvironment(req)).toBe("staging");
  });

  it("reads the query for a client that can only be given an address", () => {
    expect(mcpEnvironment(new Request("https://p.example/mcp?environment=qa"))).toBe("qa");
  });

  it("prefers the header to the query", () => {
    const req = new Request("https://p.example/mcp?environment=qa", {
      headers: { "X-Octo-Environment": "staging" },
    });
    expect(mcpEnvironment(req)).toBe("staging");
  });

  it("is the default with neither, and for octo", () => {
    expect(mcpEnvironment(new Request("https://p.example/mcp"))).toBe("");
    expect(mcpEnvironment(new Request("https://p.example/mcp?environment=octo"))).toBe("");
  });
});
