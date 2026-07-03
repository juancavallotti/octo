import { describe, expect, it } from "vitest";
import { toResourceFiles } from "./runResources";
import type { Resource } from "@/app/model/orchestrator";

const res = (name: string, content: string): Resource => ({
  id: `id-${name}`,
  integrationId: "int-1",
  kind: name.startsWith(".env") ? "env" : "template",
  name,
  content,
  createdAt: "2026-01-01T00:00:00Z",
  lastUpdated: "2026-01-01T00:00:00Z",
});

describe("toResourceFiles", () => {
  const all = [
    res(".env.dev", "A=1"),
    res("templates/welcome.tmpl", "hi"),
    res("templates/unused.tmpl", "nope"),
  ];

  it("keeps only the declared resources, reduced to name + content", () => {
    expect(toResourceFiles(all, [".env.dev", "templates/welcome.tmpl"])).toEqual([
      { name: ".env.dev", content: "A=1" },
      { name: "templates/welcome.tmpl", content: "hi" },
    ]);
  });

  it("returns nothing when no declared name matches", () => {
    expect(toResourceFiles(all, ["absent"])).toEqual([]);
    expect(toResourceFiles(all, [])).toEqual([]);
  });
});
