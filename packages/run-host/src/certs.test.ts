import { describe, expect, it } from "vitest";
import {
  LEAF_CONFIG,
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

  it("reports true when req lists -addext", async () => {
    expect(await opensslAvailable(async () => ({ stdout: "", stderr: " -addext val  Additional cert extension" }))).toBe(true);
  });

  it("reads the usage from a non-zero exit, as LibreSSL prints it", async () => {
    const libre: ExecFile = async () => {
      throw Object.assign(new Error("exit 1"), { stdout: "", stderr: "-addext ext  add a specific extension" });
    };
    expect(await opensslAvailable(libre)).toBe(true);
  });

  it("reports false when req cannot add extensions, as OpenSSL 1.0.2", async () => {
    const old: ExecFile = async () => {
      throw Object.assign(new Error("exit 1"), { stdout: "", stderr: "unknown option -help\n-x509 output a x509 structure" });
    };
    expect(await opensslAvailable(old)).toBe(false);
  });
});

describe("selfSignedArgs", () => {
  const args = selfSignedArgs(
    { commonName: "localhost", hosts: ["localhost", "127.0.0.1", "::1"], days: 30 },
    "/tmp/cnf",
    "/tmp/c",
    "/tmp/k",
  );

  it("puts DNS names and IPs in the subject alternative names", () => {
    expect(args).toContain("subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1");
    expect(args).toContain("/CN=localhost");
    expect(args.slice(args.indexOf("-days"), args.indexOf("-days") + 2)).toEqual(["-days", "30"]);
  });

  it("runs on its own configuration and the leaf profile, never the host's", () => {
    expect(args.slice(args.indexOf("-config"), args.indexOf("-config") + 2)).toEqual(["-config", "/tmp/cnf"]);
    expect(args.slice(args.indexOf("-extensions"), args.indexOf("-extensions") + 2)).toEqual(["-extensions", "leaf"]);
  });
});

describe("LEAF_CONFIG", () => {
  it("makes a server certificate that is not a CA", () => {
    expect(LEAF_CONFIG).toContain("basicConstraints = critical,CA:FALSE");
    expect(LEAF_CONFIG).toContain("extendedKeyUsage = serverAuth");
  });
});

describe("validateRequest", () => {
  it.each([
    [{ commonName: "a/b", hosts: ["a"], days: 1 }],
    [{ commonName: "a", hosts: [], days: 1 }],
    [{ commonName: "a", hosts: ["a,b"], days: 1 }],
    [{ commonName: "a", hosts: ["a"], days: 0 }],
    [{ commonName: "a", hosts: ["localhost:8443"], days: 1 }],
    [{ commonName: "a", hosts: ["127.0.0.1:8443"], days: 1 }],
  ])("rejects %j", (req) => {
    expect(() => validateRequest(req)).toThrow();
  });

  it("accepts DNS names, wildcards and bare IPv4 and IPv6 addresses", () => {
    const req = { commonName: "localhost", hosts: ["localhost", "*.example.com", "127.0.0.1", "::1"], days: 1 };
    expect(validateRequest(req)).toEqual(req);
  });

  it("returns a copy, so a later change to the request reaches nothing", () => {
    const req = { commonName: "localhost", hosts: ["localhost"], days: 1 };
    const checked = validateRequest(req);
    req.hosts.push("bad,host");
    expect(checked.hosts).toEqual(["localhost"]);
  });
});

describe("mintSelfSigned", () => {
  it("builds the arguments from the request as validated", async () => {
    const req = { commonName: "localhost", hosts: ["localhost"], days: 1 };
    let seen: string[] = [];
    const exec: ExecFile = async (_file, args) => {
      seen = args;
      throw new Error("stop");
    };
    const pending = mintSelfSigned(req, exec);
    req.hosts.push("evil,DNS:attacker");
    await expect(pending).rejects.toThrow("stop");
    expect(seen).toContain("subjectAltName=DNS:localhost");
  });

  it("surfaces a missing openssl as an error", async () => {
    await expect(
      mintSelfSigned({ commonName: "localhost", hosts: ["localhost"], days: 1 }, missing),
    ).rejects.toThrow(/ENOENT/);
  });
});
