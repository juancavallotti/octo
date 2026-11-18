import { describe, expect, it } from "vitest";
import {
  mintSelfSigned,
  opensslAvailable,
  selfSignedArgs,
  validateRequest,
  type ExecFile,
} from "./certs";

const missing: ExecFile = async () => {
  throw Object.assign(new Error("spawn openssl ENOENT"), { code: "ENOENT" });
};

describe("opensslAvailable", () => {
  it("reports false when openssl cannot be spawned", async () => {
    expect(await opensslAvailable(missing)).toBe(false);
  });

  it("reports true when openssl answers", async () => {
    expect(await opensslAvailable(async () => ({ stdout: "OpenSSL 3" }))).toBe(true);
  });
});

describe("selfSignedArgs", () => {
  it("puts DNS names and IPs in the subject alternative names", () => {
    const args = selfSignedArgs(
      { commonName: "localhost", hosts: ["localhost", "127.0.0.1"], days: 30 },
      "/tmp/c",
      "/tmp/k",
    );
    expect(args).toContain("subjectAltName=DNS:localhost,IP:127.0.0.1");
    expect(args).toContain("/CN=localhost");
    expect(args.slice(args.indexOf("-days"), args.indexOf("-days") + 2)).toEqual(["-days", "30"]);
  });
});

describe("validateRequest", () => {
  it.each([
    [{ commonName: "a/b", hosts: ["a"], days: 1 }],
    [{ commonName: "a", hosts: [], days: 1 }],
    [{ commonName: "a", hosts: ["a,b"], days: 1 }],
    [{ commonName: "a", hosts: ["a"], days: 0 }],
  ])("rejects %j", (req) => {
    expect(() => validateRequest(req)).toThrow();
  });
});

describe("mintSelfSigned", () => {
  it("surfaces a missing openssl as an error", async () => {
    await expect(
      mintSelfSigned({ commonName: "localhost", hosts: ["localhost"], days: 1 }, missing),
    ).rejects.toThrow(/ENOENT/);
  });
});
