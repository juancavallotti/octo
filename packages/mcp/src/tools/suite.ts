import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import {
  appendCase,
  hasComments,
  parseSuite,
  serializeSuite,
  suiteFileName,
  updateCase,
  validateSuite,
  type Suite,
  type SuiteCase,
  type SuiteIssue,
} from "@octo/editor/runtime";
import type { OctoMcpConfig, SuiteStore } from "../backend";
import type { NamespaceResolver } from "../namespace";
import type { RunHostPort } from "../run-host";
import { mockSpecSchema } from "../mocks";
import { errorResult, guard, jsonResult, textResult } from "../result";

/**
 * The test-authoring tools: write a flow's dolphin suite, and run it.
 *
 * `run_tests` is what makes the rest worth having. An agent that can write a test but
 * not run it has produced a plausible-looking file; one that can run it hands over
 * something it has watched pass. That is the difference between a transcript and a
 * durable artifact, which is the whole point of the issue these tools close.
 *
 * A suite is a REAL file — `<flow>_test.yaml`, committed, run by CI and by `dolphin test`
 * in a terminal, and shown in the editor's Testing tab. It is not the editor's meta file
 * next door: that one is scratch nobody else reads.
 *
 * **Values throughout.** A suite holds bodies and variables as values, and so do these
 * schemas — no JSON-in-a-string anywhere, unlike the flow-meta tools, whose file stores
 * them as text. The descriptions say so on both sides, because an agent that has used one
 * will assume the other.
 */
export function registerSuiteTools(
  server: McpServer,
  config: OctoMcpConfig,
  runHost: RunHostPort,
  resolveNamespace: NamespaceResolver,
): void {
  const { store, suiteStore } = config;
  if (!suiteStore) return;

  server.registerTool(
    "list_test_suites",
    {
      title: "List test suites",
      description:
        "List the dolphin test suites stored for an integration as { flow, file, cases }, where `cases` are the case names in the file. One suite per flow. Use `get_test_suite` to read one, and `run_tests` to run them.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) =>
      guard(async () => {
        const suites = await suiteStore.list(id);
        return jsonResult(
          suites.map(({ flow, content }) => {
            const { suite, issues } = parseSuite(content);
            const broken = issues.filter((i) => i.severity === "error");
            return {
              flow,
              file: suiteFileName(flow),
              cases: suite.cases.map((c) => c.name),
              ...(broken.length > 0
                ? { problems: broken.map((i) => i.message) }
                : {}),
            };
          }),
        );
      }),
  );

  server.registerTool(
    "get_test_suite",
    {
      title: "Get a test suite",
      description:
        "Read one flow's test suite as YAML, exactly as it is stored — comments and all. Edit it and write it back with `set_test_suite` to keep every byte you did not change; use `set_test_case` when you only want to add or replace one case. Any problems dolphin would refuse the file over are reported alongside it.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The flow the suite tests."),
      },
    },
    ({ id, flow }) =>
      guard(async () => {
        const content = await contentFor(suiteStore, id, flow);
        const issues = parseSuite(content).issues;
        const result = textResult(content);
        return issues.length === 0 ? result : withNotes(result, issues);
      }),
  );

  server.registerTool(
    "set_test_suite",
    {
      title: "Write a test suite",
      description:
        "Write a whole test suite, byte for byte — the way to author one with comments, or to edit the YAML `get_test_suite` returned. The flow under test is the file's own `flow:` key, and it decides which flow's suite this becomes; there is no separate argument to disagree with it.\n\nThe file is checked the way dolphin checks it before anything is saved, so a suite that would not load costs you nothing. Note that dolphin decodes with unknown keys REJECTED: a misspelled `spys:` does not go quietly, it takes the whole file down — which is deliberate, since a typo that was ignored would look exactly like a test that passes.\n\nA suite is a real, committed file (`<flow>_test.yaml`), run by CI and by `dolphin test` in a terminal, and shown in the editor's Testing tab.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        content: z
          .string()
          .min(1)
          .describe("The suite YAML: `flow:`, optional `inputs:`/`mocks:`/`env:`/`timeout:`, and `cases:`."),
      },
    },
    ({ id, content }) =>
      guard(async () => {
        const { suite, issues } = parseSuite(content);
        const refusal = refuseOn(issues);
        if (refusal) return refusal;
        await suiteStore.save(id, suite.flow, content);
        return withNotes(jsonResult(saved(suite)), issues);
      }),
  );

  server.registerTool(
    "set_test_case",
    {
      title: "Write one test case",
      description:
        "Add a case to a flow's suite, or replace the one with the same name — creating the suite if the flow has none yet. Everything the case does not mention is left as it was.\n\nBodies and variables are VALUES here, not strings of JSON. Note the asymmetry dolphin applies and nothing else will tell you: `expect.body` is an EXACT deep-equal of the whole body, while `expect.vars` is a SUBSET — only the variables you name are checked. A body carrying a generated id or a timestamp will therefore fail on its next run; assert those with `expect.that` instead.\n\nAn absent `expect` is not an empty test: it asserts that the flow completed without failing, which is the assertion most worth having first.\n\nThis rewrites the file from its parsed form, so **comments in the suite are lost** — use `set_test_suite` when the file has comments worth keeping. It is refused outright if the file holds anything the parser cannot represent, rather than dropping it.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The flow under test."),
        case: caseSchema.describe("The case to add or replace, by name."),
      },
    },
    ({ id, flow, case: input }) =>
      guard(async () => {
        const before = await suiteFor(suiteStore, id, flow);
        const refusal = refuseOn(before.issues);
        if (refusal) return refusal;

        const next = writeCase(before.suite, input as SuiteCase);
        const issues = validateSuite(next);
        const bad = refuseOn(issues);
        if (bad) return bad;

        await suiteStore.save(id, flow, serializeSuite(next));
        return withNotes(
          jsonResult(saved(next)),
          before.content && hasComments(before.content)
            ? [
                {
                  message:
                    "the suite's comments were dropped: this rewrites the file from its parsed form. Use set_test_suite to author a commented one.",
                  severity: "warning" as const,
                },
                ...issues,
              ]
            : issues,
        );
      }),
  );

  server.registerTool(
    "delete_test_case",
    {
      title: "Delete a test case",
      description:
        "Remove one case from a flow's suite by name. Comments in the file are lost, as with `set_test_case`. Returns the cases that remain.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The flow under test."),
        name: z.string().min(1).describe("The case name to remove."),
      },
    },
    ({ id, flow, name }) =>
      guard(async () => {
        const before = await suiteFor(suiteStore, id, flow);
        const refusal = refuseOn(before.issues);
        if (refusal) return refusal;
        if (!before.suite.cases.some((c) => c.name === name)) {
          return errorResult(
            `no case named ${JSON.stringify(name)} in ${suiteFileName(flow)}. It has: ${before.suite.cases.map((c) => c.name).join(", ") || "none"}.`,
          );
        }

        const next = {
          ...before.suite,
          cases: before.suite.cases.filter((c) => c.name !== name),
        };
        await suiteStore.save(id, flow, serializeSuite(next));
        return jsonResult(saved(next));
      }),
  );

  server.registerTool(
    "run_tests",
    {
      title: "Run the tests",
      description:
        "Run an integration's dolphin test suites and report what happened — the way to check that a test you just wrote actually holds, rather than handing over one that merely looks right.\n\nReturns { ok, timedOut, totals, suites, logs }. **`ok` means a report came back, NOT that the tests passed**: dolphin exits non-zero for a failing case, which is an ordinary outcome of running tests. The verdict is `totals` — read `failed` and `errored`. A FAILED case is a flow that did not do what the case says; an ERRORED case is a suite dolphin could not run (a bad address, an input that does not exist), so fix the test, not the flow.\n\nEvery case is reported with its status; a case that did not pass also carries its `failures` (each with the detail dolphin printed) and its `outcome` — the message the flow actually produced — which is what you compare against what the case expected.\n\nEach case runs the flow in its own process, with the suite's `env:` and mocks applied. Sources are never started, so a case supplies whatever a real trigger would have set.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z
          .string()
          .min(1)
          .optional()
          .describe("Run only this flow's suite; omit to run every suite."),
      },
    },
    ({ id, flow }, extra) =>
      guard(async () => {
        const rec = await store.get(id);
        const all = await suiteStore.list(id);
        const chosen = flow === undefined ? all : all.filter((s) => s.flow === flow);
        if (chosen.length === 0) {
          return errorResult(
            flow === undefined
              ? "this integration has no test suites yet — write one with `set_test_case` or `set_test_suite`."
              : `no test suite for "${flow}". Suites exist for: ${all.map((s) => s.flow).join(", ") || "no flows"}.`,
          );
        }

        const outcome = await runHost.test(resolveNamespace(extra.sessionId), {
          yaml: rec.definition,
          suites: chosen.map((s) => ({ name: s.flow, content: s.content })),
          resources: config.resources?.(id),
        });
        // `ok: false` is the run itself failing — no dolphin, an unreadable report, a
        // config that would not load. A failing TEST is a successful run, and comes back
        // below with the tally that says so.
        if (!outcome.ok) {
          return errorResult(
            outcome.error ??
              (outcome.timedOut
                ? "the test run did not finish in time"
                : outcome.logs.join("\n")) ??
              "the tests could not be run",
          );
        }
        return jsonResult({
          ok: outcome.ok,
          timedOut: outcome.timedOut,
          totals: outcome.totals,
          suites: outcome.suites.map((s) => ({
            flow: s.flow,
            cases: s.cases.map(reportedCase),
          })),
          logs: outcome.logs,
        });
      }),
  );
}

/**
 * What a message must look like. One schema for every place the format asks — a case's
 * expectation, and what a watched block saw going in and coming out — because dolphin
 * matches all of them with the same code.
 */
const messageExpectSchema = z.object({
  body: z
    .unknown()
    .optional()
    .describe(
      "EXACT: the whole body, deep-equal. A body carrying a generated id or timestamp will fail on the next run — assert those with `that` instead.",
    ),
  vars: z
    .record(z.string(), z.unknown())
    .optional()
    .describe(
      "SUBSET: only the variables named here are checked, so unrelated ones the engine adds do not break the test.",
    ),
  that: z
    .array(z.string())
    .optional()
    .describe(
      "CEL over the message; every expression must be true, e.g. `body.total > 0`. `body` and `vars` are bound; `env` is NOT — dolphin never loads the config, so an expression reading it fails.",
    ),
});

const expectSchema = messageExpectSchema.extend({
  dropped: z
    .boolean()
    .optional()
    .describe("The flow filtered the message out and returned nothing."),
  error: z
    .string()
    .optional()
    .describe("The flow failed, with a message CONTAINING this text."),
});

const recordSchema = z.object({
  input: messageExpectSchema.optional().describe("What the block received."),
  output: messageExpectSchema.optional().describe("What it produced."),
  dropped: z.boolean().optional().describe("It filtered the message out."),
  error: z.string().optional().describe("It failed, with a message containing this."),
});

const spyExpectSchema = z.object({
  count: z
    .number()
    .int()
    .min(0)
    .optional()
    .describe(
      "The exact number of crossings. `0` asserts the block never ran; omit the field to assert nothing about how often.",
    ),
  records: z
    .array(recordSchema)
    .optional()
    .describe(
      "Positional: the i-th crossing. Asserting on fewer crossings than happened is fine.",
    ),
});

const caseSchema = z.object({
  name: z
    .string()
    .min(1)
    .describe("Unique within the suite — dolphin refuses a file with two of a name."),
  input: z
    .union([
      z.string().describe("The name of an input declared in the file's `inputs:`."),
      z.object({
        data: z.unknown().optional().describe("The message body, as a value."),
        vars: z
          .record(z.string(), z.unknown())
          .optional()
          .describe(
            "Variables to seed the message with. No source runs, so a flow reading `vars.authorization` needs them here.",
          ),
      }),
    ])
    .optional()
    .describe("What the flow is called with: a shared input's name, or an inline message."),
  mocks: z
    .record(z.string(), mockSpecSchema)
    .optional()
    .describe(
      "Blocks to stand in for, by address. REPLACES the file-level mock on that address whole — never merged with it — so a case states its world completely.",
    ),
  env: z
    .record(z.string(), z.string())
    .optional()
    .describe("Environment for this case, laid over the file's variable by variable."),
  expect: expectSchema
    .optional()
    .describe(
      "What the flow should have done: produced a message, dropped it, or failed — exactly one of those. Omit it to assert only that the flow completed without failing.",
    ),
  spies: z
    .record(z.string(), spyExpectSchema)
    .optional()
    .describe("What each watched block should have seen, by address."),
  timeout: z.string().optional().describe("A Go duration bounding this case, e.g. `30s`."),
  skip: z
    .string()
    .optional()
    .describe("A reason. The case is reported as skipped and never run."),
});

/** One case as reported back: everything about a case that did not pass, little else. */
function reportedCase(c: {
  name: string;
  status: string;
  elapsedMs: number;
  summary?: string;
  failures?: unknown[];
  error?: string;
  stderr?: string;
  skip?: string;
  outcome?: unknown;
}) {
  const base = { name: c.name, status: c.status, elapsedMs: c.elapsedMs };
  if (c.status === "passed") return base;
  return {
    ...base,
    ...(c.summary ? { summary: c.summary } : {}),
    ...(c.failures?.length ? { failures: c.failures } : {}),
    ...(c.error ? { error: c.error } : {}),
    ...(c.stderr ? { stderr: c.stderr } : {}),
    ...(c.skip ? { skip: c.skip } : {}),
    // What the flow actually produced. Only for a case that did not pass, where it is
    // the other half of the comparison; on a passing case it is noise.
    ...(c.outcome ? { outcome: c.outcome } : {}),
  };
}

/** The stored YAML for one flow's suite, or "" when it has none. */
async function contentFor(
  suiteStore: SuiteStore,
  id: string,
  flow: string,
): Promise<string> {
  const suites = await suiteStore.list(id);
  const found = suites.find((s) => s.flow === flow);
  if (!found) {
    throw new Error(
      `no test suite for "${flow}". Suites exist for: ${suites.map((s) => s.flow).join(", ") || "no flows"}.`,
    );
  }
  return found.content;
}

/** The parsed suite for a flow, or an empty one when the flow has no suite yet. */
async function suiteFor(
  suiteStore: SuiteStore,
  id: string,
  flow: string,
): Promise<{ suite: Suite; issues: SuiteIssue[]; content: string }> {
  const found = (await suiteStore.list(id)).find((s) => s.flow === flow);
  if (!found) return { suite: { flow, cases: [] }, issues: [], content: "" };
  return { ...parseSuite(found.content), content: found.content };
}

/** Replace the case of that name, or append it. */
function writeCase(suite: Suite, c: SuiteCase): Suite {
  const at = suite.cases.findIndex((existing) => existing.name === c.name);
  return at < 0 ? appendCase(suite, c) : updateCase(suite, at, c);
}

/**
 * Refuse a write over anything dolphin would refuse the file over, saving nothing.
 *
 * The same bargain the flow tools make: these tools exist so an agent can edit a suite
 * without holding the whole file in its head, which means it also cannot see what it
 * just broke. A suite that will not load takes every case in it down, not just the one
 * that is wrong.
 */
function refuseOn(issues: SuiteIssue[]): CallToolResult | null {
  const errors = issues.filter((i) => i.severity === "error");
  if (errors.length === 0) return null;
  return errorResult(
    `dolphin would refuse this suite, so nothing was saved:\n• ${errors.map((i) => i.message).join("\n• ")}`,
  );
}

/** Warnings and other notes, as a second content block beside the result. */
function withNotes(result: CallToolResult, issues: SuiteIssue[]): CallToolResult {
  const notes = issues.filter((i) => i.severity !== "error");
  if (notes.length === 0) return result;
  return {
    ...result,
    content: [
      ...result.content,
      { type: "text" as const, text: `Note:\n• ${notes.map((i) => i.message).join("\n• ")}` },
    ],
  };
}

/** What a write reports back: where it landed and what is in it now. */
function saved(suite: Suite) {
  return {
    flow: suite.flow,
    file: suiteFileName(suite.flow),
    cases: suite.cases.map((c) => c.name),
  };
}
