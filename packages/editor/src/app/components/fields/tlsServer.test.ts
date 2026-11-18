import { describe, expect, it } from "vitest";
import { mintedNames, parseHosts, withMintedPair } from "./tlsServer";

describe("parseHosts", () => {
  it("splits and trims, dropping blanks", () => {
    expect(parseHosts(" localhost, 127.0.0.1 ,, ")).toEqual(["localhost", "127.0.0.1"]);
  });
});

describe("mintedNames", () => {
  it("names the pair after the common name", () => {
    expect(mintedNames("localhost", [])).toEqual({
      cert: "tls/localhost.crt",
      key: "tls/localhost.key",
    });
  });

  it("numbers past a pair already stored", () => {
    expect(mintedNames("localhost", ["tls/localhost.crt"]).cert).toBe("tls/localhost-2.crt");
  });

  it("replaces characters a resource path should not carry", () => {
    expect(mintedNames("*.example.com", []).cert).toBe("tls/_.example.com.crt");
  });
});

describe("withMintedPair", () => {
  it("replaces every other certificate source and keeps the rest", () => {
    expect(
      withMintedPair(
        { port: 8443, cert: "pem", key: "pem", certFile: "a", keyFile: "b" },
        { cert: "tls/x.crt", key: "tls/x.key" },
      ),
    ).toEqual({ port: 8443, certResource: "tls/x.crt", keyResource: "tls/x.key" });
  });
});
