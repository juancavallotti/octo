/**
 * What one flow run leaves behind, and how the runner's report becomes it.
 *
 * Pure, and separate from the provider that holds them: reading a runner's answer is
 * neither React's business nor a matter of state, and it is the part most worth being
 * able to reason about on its own — three of the outcomes here mean "nothing came back"
 * and mean entirely different things by it.
 */

import type { TestInput } from "../meta/types";
import type { RunSetup } from "../suite/promote";
import type { FlowRunOutcome, SpyTrace } from "./transport";

export type FlowRunStatus =
  | "running"
  /** The flow completed and produced a message. */
  | "ok"
  /** The flow filtered the message — a block returned nothing. No output, not a failure. */
  | "dropped"
  /** A breakpoint the flow never got to, because it took another branch. Not a failure. */
  | "not-reached"
  /** The flow failed, or the run could not be made. */
  | "error"
  | "timeout";

export interface FlowRunEntry {
  id: string;
  flowName: string;
  /** The input it was run with, when it was run with a saved one. */
  inputName?: string;
  /** The block it was told to stop at, when this was a run-to-here. */
  breakpointLabel?: string;
  status: FlowRunStatus;
  /** The flow's result body, or the message captured at the breakpoint. */
  output?: unknown;
  /** Why it failed — the flow's own error, or why the run could not be made. */
  error?: string;
  /**
   * What the run was made with, so it can be saved as a test case afterwards. The name
   * of the input is not enough for that: a case has to carry the message itself, and the
   * mocks that were standing in at the time.
   */
  ran?: RunSetup;
}

/**
 * Read the flow's result message — `{event_id, variables, body}` — which `octo invoke`
 * prints as JSON on stdout. It is the same shape a breakpoint reports, so the Results
 * tab shows a finished run and a run stopped at a block in the same terms.
 */
export function parseOutput(text: string): unknown {
  const trimmed = text.trim();
  if (trimmed === "") return undefined;
  try {
    return JSON.parse(trimmed);
  } catch {
    return trimmed; // not JSON: show it as it came
  }
}

/**
 * Turn what the runner reported into one status plus what to show for it. The three
 * "nothing came back" cases are meaningfully different and must not be flattened into a
 * single failure: a dropped message means a filter rejected it, an unreached breakpoint
 * means the flow took another branch, and only a real error is an error.
 */
export function classify(
  outcome: FlowRunOutcome,
  breaking: boolean,
): Pick<FlowRunEntry, "status" | "output" | "error"> {
  if (outcome.timedOut) return { status: "timeout", error: "The flow did not finish in time." };
  if (!outcome.ok) {
    return {
      status: "error",
      // A run that could not be made reports why; otherwise the runner's stderr is the
      // only account we have of what went wrong.
      error: outcome.error ?? outcome.logs.join("\n") ?? "The run failed.",
    };
  }

  if (breaking) {
    const bp = outcome.breakpoint;
    if (!bp) return { status: "error", error: "The runner reported no breakpoint." };
    if (bp.error) return { status: "error", error: bp.error };
    if (!bp.reached) return { status: "not-reached" };
    return { status: "ok", output: bp.message };
  }

  if (outcome.dropped) return { status: "dropped" };
  return { status: "ok", output: parseOutput(outcome.output) };
}

/**
 * The message a run was called with, as values.
 *
 * A saved input holds JSON *text* — it is edited in a box, and half-written text is the
 * thing that has to survive — while a test case holds values. This is where the one
 * becomes the other, on the way to a result that can be promoted. Text that will not
 * parse yields nothing rather than a body that is a string of JSON.
 */
export function inputValues(input: TestInput | undefined): RunSetup["input"] {
  if (!input) return undefined;
  const data = parseJson(input.data);
  const vars = parseJson(input.vars) as Record<string, unknown> | undefined;
  if (data === undefined && vars === undefined) return undefined;
  return { ...(data === undefined ? {} : { data }), ...(vars === undefined ? {} : { vars }) };
}

function parseJson(text: string | undefined): unknown {
  if (text === undefined || text.trim() === "") return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

/**
 * How many messages crossed each spied block on THIS run — the count a promoted case
 * asserts. Every requested address is counted, including the ones that reported nothing:
 * a block that never ran is a result, and `count: 0` is the assertion that says so.
 */
export function crossings(
  addresses: string[],
  traces: SpyTrace[] | undefined,
): Pick<RunSetup, "spies"> {
  if (addresses.length === 0) return {};
  return {
    spies: Object.fromEntries(
      addresses.map((address) => [
        address,
        traces?.find((t) => t.address === address)?.records.length ?? 0,
      ]),
    ),
  };
}
