import { describe, expect, it } from "vitest";
import { currentRuntime, imageTag, needsUpgrade } from "./runtimeRelease";

describe("imageTag", () => {
  it("takes the tag, and is not fooled by a registry port", () => {
    expect(imageTag("octo-runtime:0.8.8")).toBe("0.8.8");
    expect(imageTag("ghcr.io/juancavallotti/octo-runtime:0.8.8")).toBe("0.8.8");
    expect(imageTag("registry:5000/octo/runtime:dev")).toBe("dev");
  });

  it("has nothing to show for an untagged or digest-pinned reference", () => {
    expect(imageTag("registry:5000/octo/runtime")).toBe("");
    expect(imageTag("octo-runtime")).toBe("");
    expect(imageTag("")).toBe("");
    // The digest's own colon is not a tag separator, in either spelling: a deploy
    // pinned by digest gives the first, and the kubelet reports that pod's image
    // as the second.
    expect(imageTag("octo-runtime@sha256:abcd")).toBe("");
    expect(imageTag("ghcr.io/octo/runtime:0.8.8@sha256:abcd")).toBe("");
    expect(
      imageTag("sha256:ed08b693c518be5d6995e2e2edd6bb8ab42972a34a2c375cd7e7a85aecf8e210"),
    ).toBe("");
  });
});

describe("needsUpgrade", () => {
  it("is true when the deployment's runtime is the older release", () => {
    expect(needsUpgrade("0.8.5", "0.8.8")).toBe(true);
    expect(needsUpgrade("0.9.0", "0.10.0")).toBe(true);
    expect(needsUpgrade("v1.2.3", "1.3.0")).toBe(true);
    expect(needsUpgrade("0.8", "0.8.1")).toBe(true);
    expect(needsUpgrade("0.8.8", "0.8.8")).toBe(false);
  });

  // Someone rolled a deployment onto a build the chart has not caught up with.
  // Telling them to "upgrade" to the version they are already past is worse than
  // saying nothing.
  it("does not call a newer runtime old", () => {
    expect(needsUpgrade("0.8.9", "0.8.8")).toBe(false);
    expect(needsUpgrade("1.0.0", "0.9.9")).toBe(false);
  });

  it("says nothing about tags it cannot order", () => {
    expect(needsUpgrade("dev", "0.8.8")).toBe(false);
    expect(needsUpgrade("0.8.5", "main")).toBe(false);
    expect(needsUpgrade("0.9.0-rc1", "0.9.0")).toBe(false);
  });

  // An install that never told the app which runtime it deploys must not have
  // every deployment badged as stale.
  it("claims nothing when either side is unknown", () => {
    expect(needsUpgrade(undefined, "0.8.8")).toBe(false);
    expect(needsUpgrade("0.8.5", "")).toBe(false);
  });
});

describe("currentRuntime", () => {
  it("prefers the configured version over the reference's tag", () => {
    expect(currentRuntime("0.9.0", "reg/octo-runtime:latest")).toBe("0.9.0");
  });

  // The case this exists for: the chart pinned the image by digest, so the
  // reference has no version in it at all.
  it("carries a digest-pinned install", () => {
    expect(currentRuntime("0.9.0", "reg/octo-runtime@sha256:abcd")).toBe("0.9.0");
  });

  it("falls back to the tag, and says nothing when neither knows", () => {
    expect(currentRuntime("", "reg/octo-runtime:0.8.8")).toBe("0.8.8");
    expect(currentRuntime("", "reg/octo-runtime@sha256:abcd")).toBe("");
  });
});
