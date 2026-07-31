import { z } from "zod";
import type { MockCase } from "@octo/editor/runtime";

/**
 * How a mocked block is described to an agent, and the one conversion that goes with it.
 *
 * Shared by the run tools (which send a mock to the runner) and the meta tools (which
 * save one for the editor), because they describe the same thing and an agent that has
 * used one will assume the other. Two schemas would eventually disagree about the rules
 * below, which are the ones the runtime enforces and nothing else teaches.
 *
 * **Values here, text in the file.** The runner takes a mock's `body` and `vars` as
 * values, and so does this schema — asking an agent for a *string of JSON* invites silent
 * double-escaping. The editor's meta file holds them as JSON text instead, because the
 * editor edits them in a box and half-written text has to survive a re-render. So a saved
 * mock is converted on the way in and back on the way out, and only here.
 */
export const mockCaseSchema = z.object({
  when: z
    .string()
    .optional()
    .describe(
      "CEL condition, evaluated against the message the block RECEIVED — e.g. `body.amount > 100`, `vars.tier == \"vip\"`. Required on a case; forbidden on `default`.",
    ),
  body: z
    .unknown()
    .optional()
    .describe(
      "The body the block returns. A literal value, NOT an expression. Exactly one of body/error/drop.",
    ),
  vars: z
    .record(z.string(), z.unknown())
    .optional()
    .describe(
      "Variables set on the message alongside `body`, for blocks that report through one (e.g. an http call setting vars.status). Only valid together with `body`.",
    ),
  error: z
    .string()
    .optional()
    .describe(
      "Fail the block with this message — how an error path is tested without arranging for the real block to fail. Exactly one of body/error/drop.",
    ),
  drop: z
    .boolean()
    .optional()
    .describe(
      "Filter the message out, as a filter block would. Exactly one of body/error/drop.",
    ),
});

export const mockSpecSchema = z.object({
  cases: z
    .array(mockCaseSchema)
    .optional()
    .describe("Cases tried in order; the first whose `when` holds wins."),
  default: mockCaseSchema
    .optional()
    .describe("What the block does when no case matched. Must NOT carry a `when`."),
});

/** One mock case as an agent gives it: bodies and variables as values. */
export type MockCaseInput = z.infer<typeof mockCaseSchema>;

/** A stored case read back as values, keeping raw vars text when it is not an object. */
export type StoredMockCaseInput = Omit<MockCaseInput, "vars"> & {
  vars?: MockCaseInput["vars"] | string;
};

/** A case as the editor's meta file holds it — `body` and `vars` as JSON text. */
export function toStoredCase(c: MockCaseInput): MockCase {
  return {
    ...(c.when === undefined ? {} : { when: c.when }),
    // Pretty-printed because a human opens this file, and reads the mock in the editor's
    // JSON box, which shows exactly these bytes.
    ...(c.body === undefined ? {} : { body: JSON.stringify(c.body, null, 2) }),
    ...(c.vars === undefined ? {} : { vars: JSON.stringify(c.vars, null, 2) }),
    ...(c.error === undefined ? {} : { error: c.error }),
    ...(c.drop === undefined ? {} : { drop: c.drop }),
  };
}

/**
 * A stored case back as values. Text that will not parse comes back as the text it is:
 * the file can be hand-edited, and reporting a half-written body as it stands is more
 * use than refusing to show the mock at all. `vars` is held to a stricter rule than
 * `body` — the schema says it is an object, so text that parses to anything else (a
 * number, an array) is reported as text rather than handed back as a lie about its type.
 */
export function fromStoredCase(c: MockCase): StoredMockCaseInput {
  const vars = c.vars === undefined ? undefined : parseOr(c.vars);
  return {
    ...(c.when === undefined ? {} : { when: c.when }),
    ...(c.body === undefined ? {} : { body: parseOr(c.body) }),
    ...(c.vars === undefined ? {} : { vars: isPlainObject(vars) ? vars : c.vars }),
    ...(c.error === undefined ? {} : { error: c.error }),
    ...(c.drop === undefined ? {} : { drop: c.drop }),
  };
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseOr(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}
