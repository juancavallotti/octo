// @vitest-environment node
import { once } from "node:events";
import { PassThrough } from "node:stream";
import { describe, expect, it } from "vitest";
import { LogBuffer } from "./logBuffer";

describe("log buffer", () => {
  it("splits a piped stream into lines and holds a partial trailing line", async () => {
    const buf = new LogBuffer();
    const stream = new PassThrough();
    buf.pipe(stream);
    stream.write("one\ntwo\npar");
    stream.end("tial\n");
    await once(stream, "end");
    expect(buf.snapshot().map((l) => l.text)).toEqual(["one", "two", "partial"]);
  });

  // A child that dies mid-write breaks the pipe and the stream emits 'error'. Without a
  // listener that is an unhandled 'error' event, which throws and takes the whole editor
  // process down. It must be recorded as a log line instead.
  it("records a stream error instead of crashing the process", () => {
    const buf = new LogBuffer();
    const stream = new PassThrough();
    buf.pipe(stream);
    expect(() => stream.emit("error", new Error("broken pipe"))).not.toThrow();
    expect(buf.snapshot().some((l) => l.text.includes("broken pipe"))).toBe(true);
  });

  // A runner that writes megabytes with no newline must not grow the held buffer without
  // bound. It is flushed in bounded pieces, so no single held line — and thus no partial
  // line buffer — can outgrow the cap.
  it("bounds a runaway line with no newline", async () => {
    const CAP = 1 << 20;
    const buf = new LogBuffer();
    const stream = new PassThrough();
    buf.pipe(stream);
    stream.end("x".repeat(3 * CAP)); // 3 MiB, not a single "\n"
    await once(stream, "end");

    const lines = buf.snapshot();
    expect(lines.length).toBeGreaterThanOrEqual(2); // flushed in pieces, not held whole
    for (const l of lines) expect(l.text.length).toBeLessThanOrEqual(CAP);
  });
});
