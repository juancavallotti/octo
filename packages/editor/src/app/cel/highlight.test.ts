import { describe, expect, it } from "vitest";
import { highlightCel } from "./highlight";

describe("highlightCel", () => {
  it("tokenizes a representative expression without throwing", () => {
    const html = highlightCel("body.items.filter(x, x.qty > 0)");
    expect(html).toContain("token");
    // `body` is an in-scope variable, `filter` is a call.
    expect(html).toContain("token variable");
    expect(html).toContain("token function");
    expect(html).toContain("token number");
  });

  it("highlights strings and operators", () => {
    const html = highlightCel("env.API_BASE_URL != '' && now - duration('1h')");
    expect(html).toContain("token string");
    expect(html).toContain("token operator");
    expect(html).toContain("token variable"); // env, now
    expect(html).toContain("token function"); // duration(
  });

  it("marks booleans and null as keywords/booleans", () => {
    expect(highlightCel("true")).toContain("token boolean");
    expect(highlightCel("x in vars")).toContain("token keyword");
  });
});
