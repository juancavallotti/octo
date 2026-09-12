import type { BlockMock, EncodedShape, ObservedEntry, ObservedMessage, TestInput } from "../meta/types";
import type { MockSpec } from "../run/transport";
import type { MessageExpect, Suite } from "../suite/types";
import { merge, shapeOfJson } from "./shape";
import type { Field, Origin, ValueShape } from "./types";

/**
 * What the workspace already says about the messages flowing through it.
 *
 * Nothing here runs anything or watches anything. It reads what the user has already
 * written down — saved test inputs, the bodies their mocks return, the cases and
 * expectations in their `_test.yaml` suites — and turns it into the shapes the scope
 * model wants. That material is the best evidence available *and* the cheapest: it is
 * authored rather than captured, already committed, and carries no question about
 * whose data it is.
 *
 * A mock is the clearest case. Somebody writing `{"chargeId": "ch_1", "status":
 * "ok"}` as what a payment block returns has described that block's output exactly,
 * for the editor's purposes, without being asked to.
 */

/** The body and variables of one message, as far as they are known. */
export interface MessageShape {
  body?: ValueShape;
  vars?: ValueShape;
}

/** What is known at one block address: what it receives, and what it produces. */
export interface AddressEvidence {
  in?: MessageShape;
  out?: MessageShape;
}

export interface Evidence {
  /** What a flow's messages start as, by flow NAME. */
  root: Map<string, MessageShape>;
  /** What a block is known to receive and produce, by runtime address. */
  at: Map<string, AddressEvidence>;
  /** What a flow is known to answer with, by flow NAME. */
  output: Map<string, MessageShape>;
}

export function emptyEvidence(): Evidence {
  return { root: new Map(), at: new Map(), output: new Map() };
}

/** JSON text, or undefined when it is absent or still being typed. */
function parse(text: string | undefined): unknown | undefined {
  if (!text || text.trim() === "") return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function shapeOf(value: unknown, origin: Origin, note: string): ValueShape | undefined {
  return value === undefined ? undefined : shapeOfJson(value, origin, note);
}

/** Combine two partial messages into what is true of both. */
export function mergeMessages(a: MessageShape | undefined, b: MessageShape | undefined): MessageShape {
  const pick = (x?: ValueShape, y?: ValueShape) => (x && y ? merge(x, y) : (x ?? y));
  return { body: pick(a?.body, b?.body), vars: pick(a?.vars, b?.vars) };
}

function addAt(evidence: Evidence, address: string, side: "in" | "out", shape: MessageShape): void {
  const existing = evidence.at.get(address) ?? {};
  evidence.at.set(address, { ...existing, [side]: mergeMessages(existing[side], shape) });
}

function addRoot(evidence: Evidence, flow: string, shape: MessageShape): void {
  evidence.root.set(flow, mergeMessages(evidence.root.get(flow), shape));
}

function addOutput(evidence: Evidence, flow: string, shape: MessageShape): void {
  evidence.output.set(flow, mergeMessages(evidence.output.get(flow), shape));
}

/**
 * Saved test inputs: what the user runs the flow with, so what its body starts as.
 *
 * Merged across every input rather than the first taken — a flow with a happy-path and
 * an error input knows the keys of both, and `merge` marks what only one has.
 */
export function fromTestInputs(inputs: readonly TestInput[]): MessageShape {
  return inputs.reduce<MessageShape>(
    (acc, input) =>
      mergeMessages(acc, {
        body: shapeOf(parse(input.data), "sample", "seen in a test input"),
        vars: shapeOf(parse(input.vars), "sample", "seen in a test input"),
      }),
    {},
  );
}

/**
 * A mock's cases: what the block returns when it is stood in for.
 *
 * `error` and `drop` cases contribute nothing — they produce no message — and a case
 * whose body is half-typed is skipped rather than guessed at.
 */
export function fromBlockMocks(mocks: readonly BlockMock[], evidence: Evidence): void {
  for (const mock of mocks) {
    const note = "returned by the mock on this block";
    for (const c of [...mock.cases, ...(mock.default ? [mock.default] : [])]) {
      const body = shapeOf(parse(c.body), "inferred", note);
      const vars = shapeOf(parse(c.vars), "inferred", note);
      if (body || vars) addAt(evidence, mock.address, "out", { body, vars });
    }
  }
}

/** The same, for a mock written in a suite — where bodies are values, not text. */
function fromMockSpecs(
  mocks: Record<string, MockSpec | null> | undefined,
  evidence: Evidence,
  note: string,
): void {
  for (const [address, spec] of Object.entries(mocks ?? {})) {
    if (!spec) continue; // null LIFTS a mock; it describes nothing.
    for (const c of [...(spec.cases ?? []), ...(spec.default ? [spec.default] : [])]) {
      const body = shapeOf(c.body, "inferred", note);
      const vars = shapeOf(c.vars, "inferred", note);
      if (body || vars) addAt(evidence, address, "out", { body, vars });
    }
  }
}

function fromExpect(expect: MessageExpect | undefined, note: string): MessageShape | undefined {
  if (!expect) return undefined;
  const body = shapeOf(expect.body, "inferred", note);
  const vars = shapeOf(expect.vars, "inferred", note);
  return body || vars ? { body, vars } : undefined;
}

/**
 * A dolphin suite, which is the richest static source there is.
 *
 * It says what the flow is called with (`inputs`, each case's `input`), what it should
 * answer (`expect`), what its blocks are stood in for with (`mocks`), and — in a spy
 * expectation — what a named block should have received and produced. All of it
 * written by hand, all of it already committed beside the flow.
 */
export function fromSuite(suite: Suite, evidence: Evidence): void {
  const flow = suite.flow;
  if (!flow) return;

  const shared = suite.inputs ?? {};
  const inputShape = (value: unknown, vars: unknown, note: string): MessageShape => ({
    body: shapeOf(value, "sample", note),
    vars: shapeOf(vars, "sample", note),
  });

  for (const input of Object.values(shared)) {
    addRoot(evidence, flow, inputShape(input.data, input.vars, "declared in the test suite"));
  }
  fromMockSpecs(suite.mocks, evidence, "returned by the suite's mock on this block");

  for (const testCase of suite.cases ?? []) {
    const input = typeof testCase.input === "string" ? shared[testCase.input] : testCase.input;
    if (input) {
      addRoot(evidence, flow, inputShape(input.data, input.vars, "sent by a test case"));
    }
    fromMockSpecs(testCase.mocks, evidence, "returned by a test case's mock on this block");

    // An expectation describes the flow's answer, which is the scope at its end.
    const answer = fromExpect(testCase.expect, "expected by a test case");
    if (answer) addOutput(evidence, flow, answer);

    for (const [address, spy] of Object.entries(testCase.spies ?? {})) {
      for (const record of spy.records ?? []) {
        const received = fromExpect(record.input, "expected at this block by a test case");
        if (received) addAt(evidence, address, "in", received);
        const produced = fromExpect(record.output, "expected from this block by a test case");
        if (produced) addAt(evidence, address, "out", produced);
      }
    }
  }
}

/** Decode a stored shape into the model's. An unknown tag means "exists, unknowable". */
function decode(shape: EncodedShape | undefined, note: string): ValueShape | undefined {
  if (!shape) return undefined;
  switch (shape.t) {
    case "list":
      return { kind: "list", of: decode(shape.of, note) ?? { kind: "unknown" } };
    case "object": {
      const fields: Record<string, Field> = {};
      for (const [name, value] of Object.entries(shape.f ?? {})) {
        const inner = decode(value, note);
        if (inner) fields[name] = { shape: inner, origin: "observed", note, certain: true };
      }
      return { kind: "object", fields, open: true };
    }
    case "bool":
      return { kind: "bool" };
    case "dyn":
      return { kind: "dyn" };
    default:
      return { kind: shape.t };
  }
}

function decodeMessage(message: ObservedMessage | undefined, note: string): MessageShape | undefined {
  if (!message) return undefined;
  const body = decode(message.body, note);
  const vars = decode(message.vars, note);
  return body || vars ? { body, vars } : undefined;
}

/**
 * Shapes a traced test run actually saw.
 *
 * The strongest evidence of the lot, and the only kind that can describe a body no
 * amount of reading the document would reveal — what an LLM answered, what a REST
 * call returned. It is a cache: it says what happened on some run, not what must
 * happen, which is why it carries its own provenance into the menu.
 */
export function fromObserved(
  observed: Record<string, ObservedEntry> | undefined,
  evidence: Evidence,
): void {
  for (const [address, entry] of Object.entries(observed ?? {})) {
    const received = decodeMessage(entry.in, "seen at this block on a test run");
    if (received) addAt(evidence, address, "in", received);
    const produced = decodeMessage(entry.out, "seen leaving this block on a test run");
    if (produced) addAt(evidence, address, "out", produced);
  }
}
