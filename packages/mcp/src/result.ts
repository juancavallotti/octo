import type { CallToolResult } from "@modelcontextprotocol/server";

/**
 * Helpers for shaping tool results. Tools return JSON as a text content block (the
 * lingua franca every MCP client renders); `structuredContent` is intentionally
 * omitted so we don't have to declare an output schema per tool. Failures come back
 * as `isError` results carrying the message, rather than thrown — the SDK would
 * otherwise surface a generic protocol error and lose our wording.
 */

/** A successful tool result carrying `data` as pretty-printed JSON text. */
export function jsonResult(data: unknown): CallToolResult {
  return { content: [{ type: "text", text: JSON.stringify(data, null, 2) }] };
}

/** A successful tool result carrying a plain text body. */
export function textResult(text: string): CallToolResult {
  return { content: [{ type: "text", text }] };
}

/** An error tool result carrying `message`. */
export function errorResult(message: string): CallToolResult {
  return { content: [{ type: "text", text: message }], isError: true };
}

/**
 * Run `fn`, returning its result; if it throws, return an {@link errorResult} with
 * the error message. Keeps every tool callback a one-liner over its core logic.
 */
export async function guard(
  fn: () => Promise<CallToolResult>,
): Promise<CallToolResult> {
  try {
    return await fn();
  } catch (err) {
    return errorResult((err as Error).message);
  }
}
