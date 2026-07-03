// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  DEV_ENV_RESOURCE,
  effectiveResourceNames,
  injectDevEnvResource,
  parseDeclaredResources,
  sameNameSet,
  stageResources,
  stagedPathFor,
  resolveAndStage,
  type ResourceFile,
} from "./resources";
import YAML from "yaml";

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

  it("stages nothing when there is no provider", async () => {
    const noProvider = await resolveAndStage(
      dir,
      "resources:\n  env:\n    - .env.dev\n",
      undefined,
    );
    expect(noProvider.staged).toEqual([]);
  });

  it("always requests .env.dev, even for a config that declares no resources", async () => {
    let asked: string[] = [];
    const { declared, staged } = await resolveAndStage(
      dir,
      "service:\n  name: t\n",
      async (names) => {
        asked = names;
        return []; // host has no dev env for this run
      },
    );
    expect(asked).toEqual([DEV_ENV_RESOURCE]);
    expect(declared).toEqual([DEV_ENV_RESOURCE]);
    expect(staged).toEqual([]);
  });
});

describe("effectiveResourceNames", () => {
  it("appends .env.dev to the declared names", () => {
    expect(effectiveResourceNames("service:\n  name: t\n")).toEqual([DEV_ENV_RESOURCE]);
    expect(
      effectiveResourceNames("resources:\n  templates:\n    - resource: t.tmpl\n"),
    ).toEqual(["t.tmpl", DEV_ENV_RESOURCE]);
  });

  it("does not duplicate an explicitly declared .env.dev", () => {
    expect(effectiveResourceNames("resources:\n  env:\n    - .env.dev\n")).toEqual([
      DEV_ENV_RESOURCE,
    ]);
  });
});

describe("injectDevEnvResource", () => {
  it("appends .env.dev last in resources.env", () => {
    const out = YAML.parse(
      injectDevEnvResource("resources:\n  env:\n    - .env.shared\n"),
    );
    expect(out.resources.env).toEqual([".env.shared", DEV_ENV_RESOURCE]);
  });

  it("adds a resources.env to a config that has none", () => {
    const out = YAML.parse(injectDevEnvResource("service:\n  name: t\n"));
    expect(out.service.name).toBe("t");
    expect(out.resources.env).toEqual([DEV_ENV_RESOURCE]);
  });

  it("is idempotent when .env.dev is already declared", () => {
    const once = injectDevEnvResource("resources:\n  env:\n    - .env.dev\n");
    expect(YAML.parse(once).resources.env).toEqual([DEV_ENV_RESOURCE]);
    expect(YAML.parse(injectDevEnvResource(once)).resources.env).toEqual([
      DEV_ENV_RESOURCE,
    ]);
  });

  it("leaves a malformed config untouched", () => {
    const bad = "resources: : :\n  bad";
    expect(injectDevEnvResource(bad)).toBe(bad);
  });
});
