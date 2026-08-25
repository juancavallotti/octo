import { describe, expect, it } from "vitest";
import { isBundleFile, nameFromFilename, slug } from "./files";

/**
 * The pure half of the file helpers: naming. The download side is DOM plumbing
 * (an object URL and a transient anchor) and is left to the browser.
 */

describe("slug", () => {
  // The orchestrator names a bundle by the same rule; a divergence here shows up
  // as a UI download named differently from the same export fetched from the API.
  it("matches the orchestrator's filename rule", () => {
    expect(slug("Order Sync")).toBe("order-sync");
    expect(slug("  Spaced  Out  ")).toBe("spaced-out");
    expect(slug("a/b\\c")).toBe("a-b-c");
  });

  it("falls back when a name slugifies to nothing", () => {
    expect(slug("!!!")).toBe("integration");
    expect(slug("")).toBe("integration");
  });
});

describe("nameFromFilename", () => {
  it("strips the path and the extension", () => {
    expect(nameFromFilename("/tmp/order-sync.yaml")).toBe("order-sync");
    expect(nameFromFilename("order-sync.yml")).toBe("order-sync");
    expect(nameFromFilename("order-sync.zip")).toBe("order-sync");
  });

  it("names a file that is nothing but an extension", () => {
    expect(nameFromFilename(".zip")).toBe("Imported integration");
  });
});

describe("isBundleFile", () => {
  // Browsers report zip under several MIME types (and sometimes none), so the
  // extension — what the user actually picked — is what decides.
  it("recognizes a bundle by its extension, whatever the reported type", () => {
    expect(isBundleFile(new File([], "order-sync.zip", { type: "" }))).toBe(true);
    expect(isBundleFile(new File([], "ORDER-SYNC.ZIP"))).toBe(true);
    expect(
      isBundleFile(new File([], "order-sync.yaml", { type: "application/zip" })),
    ).toBe(false);
  });
});
