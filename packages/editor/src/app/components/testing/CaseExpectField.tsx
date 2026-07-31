"use client";

import { useState } from "react";
import { outcomeOf, type Expectation, type Outcome } from "../../suite/types";
import MessageExpectFields from "./MessageExpectFields";
import Segmented from "./Segmented";

const OUTCOMES: { key: Outcome; label: string }[] = [
  { key: "message", label: "Message" },
  { key: "dropped", label: "Dropped" },
  { key: "error", label: "Error" },
];

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * What the flow should have done.
 *
 * A three-way switch, never three fields. dolphin rejects an expectation that states two
 * outcomes — a case wanting a body AND a failure is not a stricter test but an impossible
 * one, since a flow that failed returned no message for a body to be compared against —
 * so switching drops the fields the old outcome owned rather than stash them.
 *
 * The chosen outcome is local state, and for the usual reason: an empty `error` and no
 * `error` are the same thing in the file, so the model cannot hold "chosen, not filled in
 * yet" and the switch would snap back under the cursor. The message fields keep their own
 * drafts next door.
 */
export default function CaseExpectField({
  value,
  onChange,
}: {
  value: Expectation | undefined;
  onChange: (next: Expectation | undefined) => void;
}) {
  const [outcome, setOutcome] = useState<Outcome>(() => outcomeOf(value));

  const pick = (next: Outcome) => {
    setOutcome(next);
    if (next === "dropped") return onChange({ dropped: true });
    if (next === "error") return onChange(value?.error ? { error: value.error } : undefined);
    onChange(undefined);
  };

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <h3 className="text-xs font-semibold text-zinc-600 dark:text-zinc-300">Expect</h3>
        <Segmented options={OUTCOMES} value={outcome} onChange={pick} ariaLabel="Outcome" />
      </div>

      {outcome === "dropped" && (
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          The flow filtered the message out and returned nothing.
        </p>
      )}

      {outcome === "error" && (
        <label className="flex flex-col gap-1">
          <span className="flex items-baseline justify-between text-xs font-medium text-zinc-500">
            Error
            <span className="font-normal text-zinc-400 dark:text-zinc-500">
              the failure message must contain this
            </span>
          </span>
          <input
            type="text"
            value={value?.error ?? ""}
            placeholder="card declined"
            onChange={(e) => onChange(e.target.value ? { error: e.target.value } : undefined)}
            className={FIELD}
          />
        </label>
      )}

      {outcome === "message" && (
        <MessageExpectFields
          value={value}
          onChange={(next) => onChange(next as Expectation | undefined)}
        />
      )}
    </section>
  );
}
