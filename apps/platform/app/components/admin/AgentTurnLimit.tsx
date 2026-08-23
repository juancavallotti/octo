"use client";

import { useState } from "react";
import { Field, INPUT, SecondaryButton } from "./fields";

/**
 * How many tool-calling turns one of the agent's answers may take.
 *
 * Its own component because it is the only *edited* setting on the agent page —
 * everything else there is a button — and because the empty case carries a rule
 * worth stating once: blank means "no override", and the agent's own definition
 * decides. That is the only way back to the shipped default once a number has been
 * set, so it has to be expressible rather than merely allowed.
 *
 * The draft resets by remount rather than by an effect: the caller keys this on the
 * value in force, so a successful apply builds a fresh component seeded from what
 * came back. Syncing it in an effect instead would leave the old text on screen for
 * a render, and would fight anyone typing while a roll-out was in flight.
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
  /** The limit in force, or undefined when the definition's default is. */
  value,
  disabled,
  onApply,
}: {
  value: number | undefined;
  disabled: boolean;
  /** Called with the new limit, or 0 to clear the override. */
  onApply: (limit: number) => void;
}) {
  const applied = value ? String(value) : "";
  const [draft, setDraft] = useState(applied);

  const error = turnLimitError(draft);
  const changed = draft.trim() !== applied;

  return (
    <div className="mt-5 border-t border-black/10 pt-4 dark:border-white/10">
      <Field
        label="Turn limit"
        hint="How many tool-calling turns one answer may take before he gives up. Leave it empty to use the limit his definition ships with. Applying it replaces his pods, the same as tracing."
      >
        <div className="flex items-center gap-2">
          <input
            type="number"
            inputMode="numeric"
            min={MIN}
            max={MAX}
            value={draft}
            disabled={disabled}
            placeholder="default"
            aria-label="Turn limit"
            onChange={(e) => setDraft(e.target.value)}
            className={`${INPUT} w-28`}
          />
          <SecondaryButton
            onClick={() => onApply(draft.trim() === "" ? 0 : Number(draft))}
            disabled={disabled || !changed || error !== null}
          >
            Apply
          </SecondaryButton>
        </div>
      </Field>
      {error && <p className="mt-1 text-xs text-red-500">{error}</p>}
    </div>
  );
}
