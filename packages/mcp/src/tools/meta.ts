import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import {
  blockIdAddresses,
  emptyFlowMeta,
  fileMetaFor,
  fromDefinitionYaml,
  isValidCase,
  parseEditorMeta,
  serializeEditorMeta,
  withFileMeta,
  type BlockMock,
  type FlowMeta,
  type TestInput,
} from "@octo/editor/runtime";
import type { IntegrationStore, MetaStore, OctoMcpConfig } from "../backend";
import { fromStoredCase, mockCaseSchema, toStoredCase } from "../mocks";
import { guard, jsonResult } from "../result";

/**
 * The flow-meta tools: the editor's saved test inputs, block mocks and spies.
 *
 * This is what turns a debugging session into something that outlives it. An agent that
 * has just worked out which block to stand in for, and with what, can leave the setup in
 * the editor instead of describing it in prose — the user opens the canvas and finds the
 * mocks already placed, the inputs already saved, and the ▶ menu ready to run.
 *
 * `list_block_addresses` is registered whether or not the host keeps a meta file: it is
 * what makes every address-taking tool usable, `invoke_flow`'s `spies` and `mocks`
 * included.
 *
 * Two things these tools are strict about:
 *
 *   **An address is refused, never invented.** A mock keyed by an address no block
 *   answers to looks placed and never fires. The editor fixes this by naming the block —
 *   but that is an edit to the user's definition, and a metadata write that quietly
 *   rewrites the thing it describes is a surprise. So the error names the valid
 *   addresses and the remedy, and writes nothing.
 *
 *   **Values in, values out.** A mock's `body` and `vars` are given and returned as
 *   values here even though the file holds them as JSON text (see ../mocks). The suite
 *   tools hold values throughout, so an agent moving between the two never has to think
 *   about which is which.
 */
export function registerMetaTools(server: McpServer, config: OctoMcpConfig): void {
  const { store, metaStore } = config;

  server.registerTool(
    "list_block_addresses",
    {
      title: "List block addresses",
      description:
        "List the address of every block in an integration, grouped as { flow, addresses }. An address is how a block is named to a mock, a spy or a breakpoint — `<flow>.<block>`, descending into a composite with a bracketed branch: `orders.charge`, `orders.checkHeader[else].api-call`, `orders[error].notify`. Call this before `set_mock`, `set_spy` or `invoke_flow`'s `spies`/`breakAt` rather than composing an address by hand.\n\nA block missing from this list has no address: either its name is not unique among its siblings, or it carries a `.`, `[` or `]`. Give it a distinct, plain name in the definition (via `update_flow`) and it will appear here — nothing else can address it.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z
          .string()
          .min(1)
          .optional()
          .describe("Only this flow's addresses; omit for every flow."),
      },
    },
    ({ id, flow }) =>
      guard(async () => {
        const all = await addressesOf(store, id);
        const groups = new Map<string, string[]>();
        for (const address of all) {
          const owner = flowOf(address);
          if (flow !== undefined && owner !== flow) continue;
          groups.set(owner, [...(groups.get(owner) ?? []), address]);
        }
        return jsonResult(
          [...groups].map(([name, addresses]) => ({ flow: name, addresses })),
        );
      }),
  );

  if (!metaStore) return;

  server.registerTool(
    "get_flow_meta",
    {
      title: "Get flow meta",
      description:
        "Read the editor's saved bookkeeping for one flow: { inputs, mocks, spies }. `inputs` are the messages the flow can be run with from the editor's ▶ menu; `mocks` are the blocks stood in for, keyed by address, with their bodies as VALUES (the file holds them as JSON text; they are parsed on the way out); `spies` are the addresses being recorded. This is design-time state the editor shows — it is not part of the integration's definition and a deployed runtime never reads it.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The flow name."),
      },
    },
    ({ id, flow }) =>
      guard(async () => {
        const entry = await entryFor(metaStore, id, flow);
        return jsonResult({
          inputs: entry.inputs,
          mocks: (entry.mocks ?? []).map(readableMock),
          spies: entry.spies ?? [],
        });
      }),
  );

  server.registerTool(
    "set_test_input",
    {
      title: "Save a test input",
      description:
        "Save a message the flow can be run with from the editor's ▶ menu — the body, and the variables a source would normally have set (no source runs for a manual run, so a flow reading `vars.authorization` needs them here). Give `inputId` to replace an existing one, or omit it to add a new one. Returns the saved input, including the id to address it by later.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The flow name."),
        name: z
          .string()
          .min(1)
          .describe("What the input is called in the menu, e.g. \"a vip order\"."),
        data: z
          .unknown()
          .optional()
          .describe("The message body, as a value (not a string of JSON)."),
        vars: z
          .record(z.string(), z.unknown())
          .optional()
          .describe("Variables to seed the message with, as values."),
        inputId: z
          .string()
          .min(1)
          .optional()
          .describe("Replace the input with this id; omit to add a new one."),
      },
    },
    ({ id, flow, name, data, vars, inputId }) =>
      guard(async () => {
        const input: TestInput = {
          id: inputId ?? crypto.randomUUID(),
          name,
          ...(data === undefined ? {} : { data: JSON.stringify(data, null, 2) }),
          ...(vars === undefined ? {} : { vars: JSON.stringify(vars, null, 2) }),
        };
        await mutate(metaStore, id, flow, (entry) => ({
          ...entry,
          inputs: entry.inputs.some((i) => i.id === input.id)
            ? entry.inputs.map((i) => (i.id === input.id ? input : i))
            : [...entry.inputs, input],
        }));
        return jsonResult(input);
      }),
  );

  server.registerTool(
    "delete_test_input",
    {
      title: "Delete a test input",
      description:
        "Remove one of a flow's saved test inputs by id (see `get_flow_meta`). Returns the inputs that remain.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The flow name."),
        inputId: z.string().min(1).describe("The test input's id."),
      },
    },
    ({ id, flow, inputId }) =>
      guard(async () => {
        const entry = await mutate(metaStore, id, flow, (e) => ({
          ...e,
          inputs: e.inputs.filter((i) => i.id !== inputId),
        }));
        return jsonResult(entry.inputs);
      }),
  );

  server.registerTool(
    "set_mock",
    {
      title: "Mock a block",
      description:
        "Stand in for a block on every run the editor makes, so the real one never executes — the way to leave a flow that calls a payment API or an LLM in a state the user can run. Keyed by address (see `list_block_addresses`); the flow is taken from the address.\n\nTwo rules the runtime enforces. (1) A mock REPLACES its target, so a message matching no case FAILS the block — it does not fall through, because there is no real block left. Give a `default`, or a trailing case with `when: \"true\"`, unless an unmatched message really should be an error. (2) A block either returns a message, fails, or drops it, so each case sets exactly ONE of `body`/`error`/`drop`, and `vars` only goes alongside a `body`.\n\nBodies are given as values here. The editor's file holds them as JSON text and the conversion happens for you — unlike the test-suite tools, which hold values throughout.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        address: z
          .string()
          .min(1)
          .describe("The block's address, e.g. `orders.charge`."),
        cases: z
          .array(mockCaseSchema)
          .optional()
          .describe("Cases tried in order; the first whose `when` holds wins."),
        default: mockCaseSchema
          .optional()
          .describe("What the block does when no case matched. Must NOT carry a `when`."),
        enabled: z
          .boolean()
          .optional()
          .describe(
            "Whether the mock is active. Defaults to true; a disabled mock is kept but not applied, which is how the editor turns one off for a run without losing it.",
          ),
      },
    },
    ({ id, address, cases, default: fallback, enabled }) =>
      guard(async () => {
        requireAddress(address, await addressesOf(store, id));
        const mock: BlockMock = {
          address,
          enabled: enabled ?? true,
          cases: (cases ?? []).map(toStoredCase),
          ...(fallback ? { default: toStoredCase(fallback) } : {}),
        };
        assertUsable(mock);
        await mutate(metaStore, id, flowOf(address), (entry) => ({
          ...entry,
          mocks: [...(entry.mocks ?? []).filter((m) => m.address !== address), mock],
        }));
        return jsonResult(readableMock(mock));
      }),
  );

  server.registerTool(
    "delete_mock",
    {
      title: "Remove a mock",
      description:
        "Remove the mock on a block, so the real block runs again. Returns the mocks that remain on the flow.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        address: z.string().min(1).describe("The mocked block's address."),
      },
    },
    ({ id, address }) =>
      guard(async () => {
        const entry = await mutate(metaStore, id, flowOf(address), (e) => ({
          ...e,
          mocks: (e.mocks ?? []).filter((m) => m.address !== address),
        }));
        return jsonResult((entry.mocks ?? []).map(readableMock));
      }),
  );

  server.registerTool(
    "set_spy",
    {
      title: "Watch a block",
      description:
        "Record every message that crosses a block, without changing what the flow does. The editor shows what each spied block saw, run after run, so a user opening the canvas can see where a message changes shape. Keyed by address (see `list_block_addresses`). Note that a spy inside a MOCKED block can never fire — the mock deleted that subtree — and the runtime refuses such a run outright.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        address: z.string().min(1).describe("The block's address to watch."),
      },
    },
    ({ id, address }) =>
      guard(async () => {
        requireAddress(address, await addressesOf(store, id));
        const entry = await mutate(metaStore, id, flowOf(address), (e) => ({
          ...e,
          spies: [...new Set([...(e.spies ?? []), address])],
        }));
        return jsonResult(entry.spies ?? []);
      }),
  );

  server.registerTool(
    "delete_spy",
    {
      title: "Stop watching a block",
      description:
        "Stop recording what crosses a block. Returns the addresses still being watched.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        address: z.string().min(1).describe("The watched block's address."),
      },
    },
    ({ id, address }) =>
      guard(async () => {
        const entry = await mutate(metaStore, id, flowOf(address), (e) => ({
          ...e,
          spies: (e.spies ?? []).filter((s) => s !== address),
        }));
        return jsonResult(entry.spies ?? []);
      }),
  );
}

/** The flow an address is rooted at — its first segment, before any `.` or `[`. */
function flowOf(address: string): string {
  return address.split(/[.[]/)[0];
}

/** Every address the integration's blocks answer to. */
async function addressesOf(store: IntegrationStore, id: string): Promise<string[]> {
  const rec = await store.get(id);
  return [...blockIdAddresses(fromDefinitionYaml(rec.definition)).values()];
}

/**
 * Refuse an address no block answers to, saying which ones do.
 *
 * The alternative — writing it anyway — produces a mock that looks placed in the file
 * and on the canvas and silently never fires, which is the failure this whole seam
 * exists to avoid. Renaming the block to make the address work is not on the table
 * either: it is an edit to the user's definition, made by a tool they asked for a
 * metadata write.
 */
function requireAddress(address: string, valid: string[]): void {
  if (valid.includes(address)) return;
  const flow = flowOf(address);
  const here = valid.filter((a) => flowOf(a) === flow);
  throw new Error(
    `no block answers to "${address}". ` +
      (here.length > 0
        ? `Addresses in "${flow}": ${here.join(", ")}.`
        : `No block in "${flow}" has an address.`) +
      " A block only has one when its name is unique among its siblings and free of '.', '[' and ']' — give it one with `update_flow` and it will appear in `list_block_addresses`.",
  );
}

/** Refuse a mock spec the runtime would reject, before it reaches the file. */
function assertUsable(mock: BlockMock): void {
  if (mock.cases.length === 0 && !mock.default) {
    throw new Error(
      "a mock must say what the block does: give `cases`, a `default`, or both.",
    );
  }
  for (const [i, c] of mock.cases.entries()) {
    if (!c.when) throw new Error(`case ${i + 1} needs a \`when\` condition.`);
    if (!isValidCase(c)) throw new Error(outcomeRule(`case ${i + 1}`));
  }
  if (mock.default) {
    if (mock.default.when) {
      throw new Error("`default` must not carry a `when` — it is what runs when none matched.");
    }
    if (!isValidCase(mock.default)) throw new Error(outcomeRule("`default`"));
  }
}

const outcomeRule = (what: string) =>
  `${what} must set exactly one of \`body\`, \`error\` or \`drop\`, and \`vars\` only alongside a \`body\` — a block that failed or dropped returned no message to set variables on.`;

/** A stored mock with its bodies parsed back to values. */
function readableMock(mock: BlockMock) {
  return {
    address: mock.address,
    enabled: mock.enabled,
    cases: mock.cases.map(fromStoredCase),
    ...(mock.default ? { default: fromStoredCase(mock.default) } : {}),
  };
}

/** One flow's entry, or an empty one when nothing has been saved for it. */
async function entryFor(
  metaStore: MetaStore,
  id: string,
  flow: string,
): Promise<FlowMeta> {
  const meta = parseEditorMeta(await metaStore.load(id));
  return fileMetaFor(meta, id).flows[flow] ?? emptyFlowMeta();
}

/**
 * Read, change one flow's entry, write back. Read-modify-write on every call rather than
 * caching: the editor writes this file too, and holding a copy between tool calls would
 * mean overwriting whatever the user did in the canvas meanwhile.
 */
async function mutate(
  metaStore: MetaStore,
  id: string,
  flow: string,
  change: (entry: FlowMeta) => FlowMeta,
): Promise<FlowMeta> {
  const meta = parseEditorMeta(await metaStore.load(id));
  const file = fileMetaFor(meta, id);
  const next = change(file.flows[flow] ?? emptyFlowMeta());
  const written = withFileMeta(meta, id, {
    ...file,
    flows: { ...file.flows, [flow]: next },
  });
  await metaStore.save(id, serializeEditorMeta(written));
  return next;
}
