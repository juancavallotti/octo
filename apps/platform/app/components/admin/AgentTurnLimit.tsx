"use client";

import { Field, INPUT } from "./fields";

/**
 * How many tool-calling turns one of the agent's answers may take.
 *
 * Its own component because it is the only *edited* setting on the deployment card —
 * everything else there is a button — and because the empty case carries a rule
 * worth stating once: blank means "no override", and the agent's own definition
 * decides. That is the only way back to the shipped default once a number has been
 * set, so it has to be expressible rather than merely allowed.
 *
 * Controlled, with no draft and no Apply of its own. Both belonged to a page where
 * every section committed separately; the value now lives in the page's one draft
 * and is written by the page's one Save, together with the permission below it so
 * the pods are replaced once rather than twice.
 *
 * It still validates as you type. The bounds are the orchestrator's and it is still
 * the one that decides — this only answers a typo without a round trip that would
 * have replaced the pods to reject it.
 */

/**
 * The bounds the orchestrator enforces, mirrored so a typo is answered without a
 * round trip — and that round trip would also roll the agent's pods. They match
 * MinIterations and MaxIterationsCeiling in orchestrator/internal/agent/types.go;
 * the server is still the one that decides.
 */
const MIN = 1;
const MAX = 200;

/** What is wrong with `raw`, or null. Empty is valid: it clears the override. */
export function turnLimitError(raw: string): string | null {
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  const n = Number(trimmed);
  if (!Number.isInteger(n)) return "Whole numbers only.";
  if (n < MIN || n > MAX) return `Between ${MIN} and ${MAX}.`;
  return null;
}

export default function AgentTurnLimit({
  /** The drafted limit as typed. Empty means the definition's own default. */
  value,
  disabled,
  onChange,
}: {
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const error = turnLimitError(value);

  return (
    <div>
      <Field
        label="Turn limit"
        hint="How many tool-calling turns one answer may take before he gives up. Leave it empty to use the limit his definition ships with."
      >
        <input
          type="number"
          inputMode="numeric"
          min={MIN}
          max={MAX}
          value={value}
          disabled={disabled}
          placeholder="default"
          aria-label="Turn limit"
          onChange={(e) => onChange(e.target.value)}
          className={`${INPUT} w-28`}
        />
      </Field>
      {error && <p className="mt-1 text-xs text-red-500">{error}</p>}
    </div>
  );
}
