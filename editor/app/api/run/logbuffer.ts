import type { Readable } from "node:stream";

/**
 * A per-namespace ring buffer of runner log lines with live subscribers. Each run
 * session owns one: stdout/stderr is split into lines and pushed here, SSE clients
 * replay the buffer and subscribe to new lines, and the monotonic `seq` doubles as
 * the SSE event id so clients can dedupe across reconnects.
 */

/** Largest config inputs are tiny; this cap just bounds the in-memory log buffer. */
const MAX_LOG_LINES = 5000;

export interface LogLine {
  /** Monotonic id, used as the SSE event id so clients can order/resume. */
  seq: number;
  text: string;
}

type Listener = (line: LogLine) => void;

export class LogBuffer {
  private logs: LogLine[] = [];
  private seq = 0;
  private listeners = new Set<Listener>();

  /** Append a line, evict the oldest past the cap, and notify subscribers. */
  push(text: string): void {
    const line: LogLine = { seq: this.seq++, text };
    this.logs.push(line);
    if (this.logs.length > MAX_LOG_LINES) {
      this.logs.splice(0, this.logs.length - MAX_LOG_LINES);
    }
    for (const listener of this.listeners) {
      try {
        listener(line);
      } catch {
        // A listener whose stream has closed is harmless; it unsubscribes on cancel.
      }
    }
  }

  /** Drop buffered lines for a fresh run; `seq` stays monotonic so clients dedupe. */
  reset(): void {
    this.logs = [];
  }

  /** Replay the current buffer (oldest first). */
  snapshot(): LogLine[] {
    return [...this.logs];
  }

  /** Subscribe to new lines; returns an unsubscribe function. */
  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  /** Split a stream into lines and push each, holding any partial trailing line. */
  pipe(stream: Readable | null): void {
    if (!stream) return;
    let buffer = "";
    stream.setEncoding("utf8");
    stream.on("data", (chunk: string) => {
      buffer += chunk;
      let nl: number;
      while ((nl = buffer.indexOf("\n")) >= 0) {
        this.push(buffer.slice(0, nl).replace(/\r$/, ""));
        buffer = buffer.slice(nl + 1);
      }
    });
    stream.on("end", () => {
      if (buffer !== "") this.push(buffer.replace(/\r$/, ""));
      buffer = "";
    });
  }
}
