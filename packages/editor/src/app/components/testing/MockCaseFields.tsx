"use client";

import { useState } from "react";
import CelEditor from "../../cel/CelEditor";
import { mockOutcomeOf, type MockCaseSpec, type MockOutcome } from "../../suite/types";
import JsonValueField from "./JsonValueField";
import Segmented from "./Segmented";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

const OUTCOMES: { key: MockOutcome; label: string }[] = [
  { key: "body", label: "Body" },
  { key: "error", label: "Error" },
  { key: "drop", label: "Drop" },
];

/**
 * One canned answer from a mocked block: when it applies, and what the block does.
 *
 * The same one-outcome rule as an expectation, one level down — a block either returns a
 * message, fails, or filters it out, and the engine rejects a case that claims two. So
 * switching drops the fields the old outcome owned rather than stash them: a mock is
 * exactly the thing you fiddle with until it is right, and a stranded `error` behind a
 * `body` would be a spec the runner refuses.
 *
 * The chosen outcome is local state for the reason it always is here: an empty `error`
 * and no `error` are the same thing in the file, so the model cannot hold "chosen, not
 * filled in yet" and the switch would snap back under the cursor.
 */
export default function MockCaseFields({
  value,
  onChange,
  isDefault,
}: {
  value: MockCaseSpec;
  onChange: (next: MockCaseSpec) => void;
  /** The default has no `when`: it is what runs when no case matched. */
  isDefault?: boolean;
}) {
  const [outcome, setOutcome] = useState<MockOutcome>(() => mockOutcomeOf(value));

  const pick = (next: MockOutcome) => {
    setOutcome(next);
    const when = isDefault ? undefined : value.when;
    if (next === "drop") onChange(clean({ when, drop: true }));
    else if (next === "error") onChange(clean({ when, error: value.error ?? "" }));
    else onChange(clean({ when, body: value.body, vars: value.vars }));
  };

  return (
    <div className="flex flex-col gap-1.5">
      {!isDefault && (
        <div
          className={
            value.when?.trim()
              ? ""
              : "rounded-md ring-1 ring-amber-400/70"
          }
        >
          <CelEditor
            value={value.when ?? ""}
            onChange={(when) => onChange(clean({ ...value, when }))}
            placeholder="body.amount > 100"
            minHeight={28}
          />
        </div>
      )}

      <Segmented
        options={OUTCOMES}
        value={outcome}
        onChange={pick}
        ariaLabel="Mock outcome"
      />

      {outcome === "body" && (
        <>
          <JsonValueField
            label="Body"
            hint="what the block returns instead of running"
            value={value.body}
            placeholder='{ "charged": true }'
            minHeight={48}
            onChange={(body) => onChange(clean({ ...value, body }))}
          />
          <JsonValueField
            label="Variables"
            hint="set alongside the body"
            value={value.vars}
            objectOnly
            placeholder='{ "status": "200" }'
            minHeight={48}
            onChange={(vars) =>
              onChange(clean({ ...value, vars: vars as Record<string, unknown> }))
            }
          />
        </>
      )}

      {outcome === "error" && (
        <input
          type="text"
          value={value.error ?? ""}
          placeholder="card declined"
          onChange={(e) => onChange(clean({ ...value, error: e.target.value }))}
          className={FIELD}
        />
      )}

      {outcome === "drop" && (
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          The message is filtered out, as a filter block would.
        </p>
      )}
    </div>
  );
}

/**
 * Keep only the fields the case actually sets. The outcomes are mutually exclusive, so
 * what is being replaced has to go rather than linger as an `undefined` that shows up in
 * a round-trip comparison.
 */
function clean(c: MockCaseSpec): MockCaseSpec {
  const out: MockCaseSpec = {};
  if (c.when !== undefined) out.when = c.when;
  if (c.body !== undefined) out.body = c.body;
  if (c.vars !== undefined) out.vars = c.vars;
  if (c.error) out.error = c.error;
  if (c.drop) out.drop = true;
  return out;
}
