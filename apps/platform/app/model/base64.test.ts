import { describe, expect, it } from "vitest";
import { fromBase64, toBase64 } from "./base64";

describe("base64", () => {
  it("round-trips bytes", () => {
    const bytes = new Uint8Array([0, 1, 80, 75, 3, 4, 255, 128]);
    expect(fromBase64(toBase64(bytes))).toEqual(bytes);
  });

  it("round-trips an empty array", () => {
    expect(fromBase64(toBase64(new Uint8Array()))).toEqual(new Uint8Array());
  });

  // The conversion is chunked because spreading a whole large array into
  // String.fromCharCode overflows the call stack; this is the case that catches a
  // chunk boundary handled wrongly.
  it("round-trips a payload larger than one chunk", () => {
    const bytes = new Uint8Array(200_000);
    for (let i = 0; i < bytes.length; i++) bytes[i] = i % 256;
    expect(fromBase64(toBase64(bytes))).toEqual(bytes);
  });

  it("agrees with Node's own encoder", () => {
    const bytes = new Uint8Array([80, 75, 3, 4, 10, 0]);
    expect(toBase64(bytes)).toBe(Buffer.from(bytes).toString("base64"));
  });
});
