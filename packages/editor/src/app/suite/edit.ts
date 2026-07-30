/**
 * Immutable edits to a {@link Suite}, so the form never mutates the model it is
 * rendering.
 *
 * These live next to the model rather than in the components because the two canvas
 * bridges need them too: a promoted run result is an {@link addCase}, and both bridges
 * have to produce cases the same way the form does or the two will drift.
 */

import type { Suite, SuiteCase } from "./types";

/** Replace one case, leaving the rest of the file alone. */
export function updateCase(suite: Suite, index: number, next: SuiteCase): Suite {
  if (index < 0 || index >= suite.cases.length) return suite;
  return { ...suite, cases: suite.cases.map((c, i) => (i === index ? next : c)) };
}

/**
 * Append a case.
 *
 * It carries nothing but a name, and that is already a test: a case with no `expect`
 * asserts that the flow ran to completion without failing, which is the assertion most
 * worth having first and the one people forget to write.
 */
export function addCase(suite: Suite, name: string): Suite {
  return { ...suite, cases: [...suite.cases, { name }] };
}

export function removeCase(suite: Suite, index: number): Suite {
  return { ...suite, cases: suite.cases.filter((_, i) => i !== index) };
}

/**
 * Whether another case already answers to this name.
 *
 * Duplicate names are a dolphin LOAD error, so the file with two "it charges" cases does
 * not run one of them twice — it runs none of them, and reports nothing by that name to
 * explain why. `exceptIndex` is the case being renamed, which is allowed to keep its own.
 */
export function nameTaken(suite: Suite, name: string, exceptIndex: number): boolean {
  const wanted = name.trim();
  return suite.cases.some((c, i) => i !== exceptIndex && c.name.trim() === wanted);
}

/** A name no case in the suite has yet, so "Add case" never authors an invalid file. */
export function uniqueCaseName(suite: Suite, base = "new case"): string {
  if (!nameTaken(suite, base, -1)) return base;
  for (let n = 2; ; n++) {
    const candidate = `${base} ${n}`;
    if (!nameTaken(suite, candidate, -1)) return candidate;
  }
}
