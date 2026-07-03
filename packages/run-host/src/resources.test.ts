// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  parseDeclaredResources,
  sameNameSet,
  stageResources,
  stagedPathFor,
  resolveAndStage,
  type ResourceFile,
} from "./resources";

describe("parseDeclaredResources", () => {
  it("collects env ids and template resource ids in order, de-duped", () => {
    const yaml = [
      "resources:",
      "  env:",
      "    - .env.dev",
      "    - .env.shared",
      "    - .env.dev", // dup
      "  templates:",
      "    - resource: templates/welcome.tmpl",
      "      as: welcome",
      "    - resource: templates/bye.tmpl",
    ].join("\n");
    expect(parseDeclaredResources(yaml)).toEqual([
      ".env.dev",
      ".env.shared",
      "templates/welcome.tmpl",
      "templates/bye.tmpl",
    ]);
  });

  it("trims whitespace and ignores blank/non-string entries", () => {
    const yaml = [
      "resources:",
      "  env:",
      "    - '  .env.dev  '",
      "    - ''",
      "  templates:",
      "    - resource: '  templates/a.tmpl '",
      "    - as: no-resource-key",
    ].join("\n");
    expect(parseDeclaredResources(yaml)).toEqual([".env.dev", "templates/a.tmpl"]);
  });

  it("returns [] for a document with no resources and for malformed YAML", () => {
    expect(parseDeclaredResources("service:\n  name: t\n")).toEqual([]);
    expect(parseDeclaredResources("resources: : :\n  bad")).toEqual([]);
  });
});

describe("sameNameSet", () => {
  it("is order-insensitive and length-sensitive", () => {
    expect(sameNameSet(["a", "b"], ["b", "a"])).toBe(true);
    expect(sameNameSet(["a"], ["a", "b"])).toBe(false);
    expect(sameNameSet([], [])).toBe(true);
  });
});

describe("stagedPathFor", () => {
  it("resolves nested names under the dir", () => {
    expect(stagedPathFor("/run/ns", "templates/welcome.tmpl")).toBe(
      "/run/ns/templates/welcome.tmpl",
    );
    expect(stagedPathFor("/run/ns", ".env.dev")).toBe("/run/ns/.env.dev");
  });

  it("clamps traversal and absolute names safely under the dir (mirrors the Go loader)", () => {
    // "/" + name is cleaned as an absolute path, so leading `..` and absolute
    // paths can never climb above root — they are remapped within it.
    expect(stagedPathFor("/run/ns", "../x")).toBe("/run/ns/x");
    expect(stagedPathFor("/run/ns", "a/../../x")).toBe("/run/ns/x");
    expect(stagedPathFor("/run/ns", "/etc/passwd")).toBe("/run/ns/etc/passwd");
  });
});

describe("stageResources", () => {
  let dir: string;
  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-res-"));
  });
  afterEach(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  it("writes declared files, creating nested directories", async () => {
    const files: ResourceFile[] = [
      { name: ".env.dev", content: "A=1\n" },
      { name: "templates/welcome.tmpl", content: "hi {{.name}}" },
    ];
    const written = await stageResources(dir, files, [
      ".env.dev",
      "templates/welcome.tmpl",
    ]);
    expect(written).toHaveLength(2);
    expect(await readFile(join(dir, ".env.dev"), "utf8")).toBe("A=1\n");
    expect(await readFile(join(dir, "templates/welcome.tmpl"), "utf8")).toBe(
      "hi {{.name}}",
    );
  });

  it("skips files the config didn't declare (no smuggling)", async () => {
    const written = await stageResources(
      dir,
      [
        { name: ".env.dev", content: "A=1" },
        { name: "secret.txt", content: "nope" },
      ],
      [".env.dev"],
    );
    expect(written).toHaveLength(1);
    expect(await readdir(dir)).toEqual([".env.dev"]);
  });
});

describe("resolveAndStage", () => {
  let dir: string;
  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-res-"));
  });
  afterEach(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  it("returns the declared names and stages what the provider supplies", async () => {
    const yaml = "resources:\n  env:\n    - .env.dev\n";
    const { declared, staged } = await resolveAndStage(dir, yaml, async (names) => {
      expect(names).toEqual([".env.dev"]);
      return [{ name: ".env.dev", content: "A=1" }];
    });
    expect(declared).toEqual([".env.dev"]);
    expect(staged).toHaveLength(1);
    expect(await readFile(join(dir, ".env.dev"), "utf8")).toBe("A=1");
  });

  it("stages nothing when there is no provider or nothing is declared", async () => {
    const noProvider = await resolveAndStage(dir, "resources:\n  env:\n    - .env.dev\n", undefined);
    expect(noProvider.staged).toEqual([]);
    const nothing = await resolveAndStage(dir, "service:\n  name: t\n", async () => [
      { name: "x", content: "y" },
    ]);
    expect(nothing.declared).toEqual([]);
    expect(nothing.staged).toEqual([]);
  });
});
