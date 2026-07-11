import { isValidCase, type BlockMock, type MockCase } from "../meta/types";
import type { MockCaseSpec, MockSpec } from "./transport";

/**
 * Turning the mocks the editor saved into the mocks the runner takes.
 *
 * The two disagree on one thing, deliberately. In the meta file a case's `body` and `vars`
 * are JSON *text*, because that is what a form edits and what survives a half-finished
 * edit; the runner wants JSON *values*. This is where the one becomes the other.
 *
 * Everything here is forgiving, and that is the point: the meta file is hand-editable and
 * a mock is built a field at a time, so a spec passing through a half-written case is
 * normal rather than exceptional. A case the runner would reject is dropped here instead —
 * the runtime validates the whole set at the edge and fails the *run* if anything is
 * malformed, which would mean a user with one broken case could not exercise their flow at
 * all. Dropping it costs them that case; passing it on costs them the run.
 */

/** Parse a JSON field, or undefined when it is absent or does not parse. */
function value(text: string | undefined): unknown | undefined {
  if (text === undefined || text.trim() === "") return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

/**
 * Convert one case, or null when the runner would reject it: a case must set exactly one
 * of body/error/drop, and `vars` only rides along with a `body`.
 *
 * A `body` that does not parse takes the case with it. It cannot be silently treated as
 * absent — that would turn a body case into an outcome-less one, and the block would end
 * up standing in for nothing rather than returning what the user wrote.
 */
function toCase(c: MockCase): MockCaseSpec | null {
  if (!isValidCase(c)) return null;

  if (c.drop) return { ...(c.when ? { when: c.when } : {}), drop: true };
  if (c.error) return { ...(c.when ? { when: c.when } : {}), error: c.error };

  const body = value(c.body);
  if (body === undefined) return null; // unparseable body: the case says nothing
  const vars = value(c.vars);
  return {
    ...(c.when ? { when: c.when } : {}),
    body,
    ...(vars !== undefined ? { vars: vars as Record<string, unknown> } : {}),
  };
}

/**
 * The mocks a run is given, keyed by block address.
 *
 * A mock that ends up with no usable case and no default is left out entirely rather than
 * sent empty: the runtime rejects a spec that says nothing (`needs at least one case, or a
 * default`), and an empty mock would mean the same thing as no mock — except that it would
 * fail the run instead of doing nothing.
 */
export function toMockSpecs(mocks: BlockMock[]): Record<string, MockSpec> {
  const out: Record<string, MockSpec> = {};
  for (const mock of mocks) {
    const cases = mock.cases.map(toCase).filter((c): c is MockCaseSpec => c !== null);
    const fallback = mock.default ? toCase(mock.default) : null;
    if (cases.length === 0 && !fallback) continue;

    // The default is what runs when no case matched, so it carries no condition — the
    // runtime rejects a default that has one.
    const { when: _condition, ...fallbackOutcome } = fallback ?? {};

    out[mock.address] = {
      ...(cases.length > 0 ? { cases } : {}),
      ...(fallback ? { default: fallbackOutcome } : {}),
    };
  }
  return out;
}
