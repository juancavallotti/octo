import { describe, expect, it } from "vitest";
import { guessKind, scaffoldFor } from "./scaffold";

describe("guessKind", () => {
  it("classifies env-convention names as env, else template", () => {
    expect(guessKind(".env")).toBe("env");
    expect(guessKind(".env.staging")).toBe("env");
    expect(guessKind("config/app.env")).toBe("env");
    expect(guessKind("templates/welcome.tmpl")).toBe("template");
    expect(guessKind("data.json")).toBe("template");
  });
});

describe("scaffoldFor", () => {
  it("gives a starter body per file type", () => {
    expect(scaffoldFor("x.json")).toBe("{}\n");
    expect(scaffoldFor("page.html")).toContain("<!doctype html>");
    expect(scaffoldFor("feed.xml")).toContain("<?xml");
    expect(scaffoldFor("c.yaml")).toBe("# \n");
    expect(scaffoldFor("README.md")).toBe("# \n");
    expect(scaffoldFor(".env.dev")).toBe("# KEY=value\n");
    expect(scaffoldFor("notes.txt")).toBe("");
  });
});
