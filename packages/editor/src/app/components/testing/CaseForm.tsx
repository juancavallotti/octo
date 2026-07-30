"use client";

import { useState } from "react";
import { isValidDuration, type SuiteCase, type Suite } from "../../suite/types";
import { nameTaken } from "../../suite/edit";
import CaseInputField from "./CaseInputField";
import CaseExpectField from "./CaseExpectField";
import KeyValueField from "./KeyValueField";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * One case: what it is called, what the flow is called with, and what should have
 * happened.
 *
 * The name is the only field held locally, and it is held because a duplicate must not
 * reach the file. Two cases with one name is a dolphin LOAD error: the suite does not run
 * one of them twice, it runs NONE of them, and reports nothing by that name to explain
 * why. So a name that clashes is shown as a problem and simply not written — the model
 * keeps the last one that was valid, and nothing downstream ever sees the clash.
 *
 * Everything else writes straight through, because everything else survives the round
 * trip to YAML and back: an empty `skip` and an absent `skip` mean the same thing, so
 * there is no half-expressed state for the model to lose.
 */
export default function CaseForm({
  suite,
  index,
  onChange,
}: {
  suite: Suite;
  index: number;
  onChange: (next: SuiteCase) => void;
}) {
  const value = suite.cases[index];
  const [name, setName] = useState(value.name);

  const clash = name.trim() !== "" && nameTaken(suite, name, index);
  const blank = name.trim() === "";

  const rename = (next: string) => {
    setName(next);
    if (next.trim() === "" || nameTaken(suite, next, index)) return;
    onChange({ ...value, name: next });
  };

  const badTimeout = !!value.timeout && !isValidDuration(value.timeout);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-3">
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium text-zinc-500">Name</span>
        <input
          type="text"
          value={name}
          placeholder="it charges the card"
          onChange={(e) => rename(e.target.value)}
          className={`${FIELD} ${clash || blank ? "border-rose-400/70" : ""}`}
        />
        {blank && (
          <span className="text-[11px] text-rose-500">
            A case needs a name — it is what the report calls it.
          </span>
        )}
        {clash && (
          <span className="text-[11px] text-rose-500">
            Another case is already called that, and dolphin would refuse the whole file.
          </span>
        )}
      </label>

      <CaseInputField
        value={value.input}
        declared={Object.keys(suite.inputs ?? {})}
        onChange={(input) => onChange(withOptional(value, "input", input))}
      />

      <CaseExpectField
        value={value.expect}
        onChange={(expect) => onChange(withOptional(value, "expect", expect))}
      />

      <details className="text-xs">
        <summary className="cursor-pointer text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300">
          Environment, skip and timeout
        </summary>
        <div className="mt-2 flex flex-col gap-3">
          <KeyValueField
            label="Environment"
            // Per VARIABLE, not whole-map: a variable is a scalar, so overriding one
            // leaves the rest of the file's environment in place.
            hint="Overrides the file's, one variable at a time."
            value={value.env}
            onChange={(env) => onChange(withOptional(value, "env", env))}
            keyPlaceholder="SOME_KEY"
            valuePlaceholder="not-a-real-key"
            addLabel="Add variable"
          />
          <label className="flex flex-col gap-1">
            <span className="flex items-baseline justify-between text-xs font-medium text-zinc-500">
              Skip
              <span className="font-normal text-zinc-400 dark:text-zinc-500">
                a reason; the case is reported as skipped and never run
              </span>
            </span>
            <input
              type="text"
              value={value.skip ?? ""}
              placeholder="waiting on the sandbox account"
              onChange={(e) => onChange(withOptional(value, "skip", e.target.value || undefined))}
              className={FIELD}
            />
          </label>

          <label className="flex flex-col gap-1">
            <span className="flex items-baseline justify-between text-xs font-medium text-zinc-500">
              Timeout
              <span className="font-normal text-zinc-400 dark:text-zinc-500">
                overrides the file&apos;s, e.g. 30s
              </span>
            </span>
            <input
              type="text"
              value={value.timeout ?? ""}
              placeholder="30s"
              onChange={(e) =>
                onChange(withOptional(value, "timeout", e.target.value || undefined))
              }
              className={`${FIELD} ${badTimeout ? "border-rose-400/70" : ""}`}
            />
            {badTimeout && (
              <span className="text-[11px] text-rose-500">
                Not a Go duration — try 30s, 1m30s or 500ms.
              </span>
            )}
          </label>
        </div>
      </details>
    </div>
  );
}

/**
 * Set an optional field, or remove the key entirely when it is cleared. Leaving an
 * `undefined` behind would be invisible in the model and in YAML alike, but it shows up
 * in a deep-equal — and the round trip through the file is one of the things the tests
 * hold this form to.
 */
function withOptional<K extends keyof SuiteCase>(
  value: SuiteCase,
  key: K,
  next: SuiteCase[K] | undefined,
): SuiteCase {
  const out = { ...value };
  if (next === undefined) delete out[key];
  else out[key] = next;
  return out;
}
