import { describe, expect, it } from "vitest";
import { languageFor, languageLabel, highlight } from "./highlight";

describe("languageFor", () => {
  it("detects supported extensions and env-convention names", () => {
    expect(languageFor("a.json")?.language).toBe("json");
    expect(languageFor("page.html")?.language).toBe("markup");
    expect(languageFor("feed.xml")?.language).toBe("markup");
    expect(languageFor("config.yaml")?.language).toBe("yaml");
    expect(languageFor("config.yml")?.language).toBe("yaml");
    expect(languageFor("README.md")?.language).toBe("markdown");
    expect(languageFor("notes.markdown")?.language).toBe("markdown");
    expect(languageFor(".env")?.language).toBe("env");
    expect(languageFor(".env.staging")?.language).toBe("env");
    expect(languageFor("nested/dir/.env.prod")?.language).toBe("env");
    expect(languageFor("notes.txt")).toBeNull();
  });
});

describe("highlight", () => {
  it("highlights each supported language without throwing", () => {
    expect(highlight('{"a":1}', "x.json")).toContain("token");
    expect(highlight("<a href='x'>y</a>", "x.html")).toContain("token");
    expect(highlight("key: value\n# c", "x.yaml")).toContain("token");
    expect(highlight("# c\nAPI_KEY=secret", ".env.dev")).toContain("token");
    expect(highlight("# Title\n\n- item", "README.md")).toContain("token");
  });

  it("escapes plain text rather than interpreting markup", () => {
    expect(highlight("<b>&x", "notes.txt")).toBe("&lt;b&gt;&amp;x");
  });

  it("marks {{ }} handlebars expressions in every language, including plain", () => {
    expect(highlight("Hello {{ user.name }}", "notes.txt")).toContain(
      "token handlebars",
    );
    expect(highlight('{"a": "{{ x }}"}', "x.json")).toContain(
      "token handlebars",
    );
    expect(highlight("<p>{{ x }}</p>", "x.html")).toContain("token handlebars");
  });
});

describe("languageLabel", () => {
  it("gives a friendly label, defaulting to plain text", () => {
    expect(languageLabel("x.json")).toBe("JSON");
    expect(languageLabel("x.md")).toBe("Markdown");
    expect(languageLabel("x.txt")).toBe("plain text");
  });
});
