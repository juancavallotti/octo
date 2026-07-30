"use client";

import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import CelEditor from "../../cel/CelEditor";
import { outcomeOf, type Expectation, type Outcome } from "../../suite/types";
import JsonValueField from "./JsonValueField";
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
 * Two bits of local state, and only two, both for the same reason: the model can hold
 * only what is valid, and these are the moments where the user has expressed an intent
 * that is not valid yet. A freshly chosen `error` has no text, and an empty `error` is
 * indistinguishable from no error at all, so the model would drop it and the switch would
 * snap back under the cursor. Same for a `that:` row the moment it is added. Everything
 * else here is read straight from the model.
 */
export default function CaseExpectField({
  value,
  onChange,
}: {
  value: Expectation | undefined;
  onChange: (next: Expectation | undefined) => void;
}) {
  const [outcome, setOutcome] = useState<Outcome>(() => outcomeOf(value));
  const [that, setThat] = useState<string[]>(() => value?.that ?? []);

  const message = (next: Expectation) => onChange(clean(next));

  const pick = (next: Outcome) => {
    setOutcome(next);
    if (next === "dropped") return onChange({ dropped: true });
    if (next === "error") return onChange(clean({ error: value?.error ?? "" }));
    setThat(value?.that ?? []);
    onChange(clean({ body: value?.body, vars: value?.vars, that: value?.that }));
  };

  const writeThat = (next: string[]) => {
    setThat(next);
    // A half-typed expression is not one dolphin could compile, so only the filled-in
    // rows reach the file; the empty one stays on screen until it says something.
    message({ ...value, that: next.filter((e) => e.trim() !== "") });
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
            onChange={(e) => onChange(clean({ error: e.target.value }))}
            className={FIELD}
          />
        </label>
      )}

      {outcome === "message" && (
        <>
          <JsonValueField
            label="Body"
            // The format's most surprising rule, said where it applies rather than in a
            // doc page: body is exact and vars is not.
            hint="exact — the whole body, deep-equal"
            value={value?.body}
            placeholder='{ "charged": true }'
            onChange={(body) => message({ ...value, body })}
          />
          <JsonValueField
            label="Variables"
            hint="subset — only the ones named here"
            value={value?.vars}
            objectOnly
            placeholder='{ "status": "200" }'
            onChange={(vars) =>
              message({ ...value, vars: vars as Record<string, unknown> })
            }
          />

          <div className="flex flex-col gap-1">
            <span className="flex items-baseline justify-between text-xs font-medium text-zinc-500">
              That
              <span className="font-normal text-zinc-400 dark:text-zinc-500">
                CEL over the message; each must be true
              </span>
            </span>
            {that.map((expression, i) => (
              <div key={i} className="flex items-center gap-1">
                <div className="min-w-0 flex-1">
                  <CelEditor
                    value={expression}
                    onChange={(next) =>
                      writeThat(that.map((e, at) => (at === i ? next : e)))
                    }
                    placeholder="body.total > 0"
                    minHeight={28}
                  />
                </div>
                <button
                  type="button"
                  aria-label={`Remove expression ${i + 1}`}
                  onClick={() => writeThat(that.filter((_, at) => at !== i))}
                  className="shrink-0 text-zinc-400 hover:text-red-500"
                >
                  <Trash2 size={13} />
                </button>
              </div>
            ))}
            <button
              type="button"
              onClick={() => setThat([...that, ""])}
              className="flex items-center gap-1 self-start text-xs text-sky-600 hover:underline dark:text-sky-400"
            >
              <Plus size={13} /> Add expression
            </button>
          </div>
        </>
      )}
    </section>
  );
}

/**
 * Drop the fields that say nothing, and the expectation itself when none are left. An
 * absent `expect:` is not an absent assertion — it still asserts the flow completed and
 * did not fail — so an empty mapping would add noise without adding meaning.
 */
function clean(e: Expectation): Expectation | undefined {
  const out: Expectation = {};
  if (e.body !== undefined) out.body = e.body;
  if (e.vars && Object.keys(e.vars).length) out.vars = e.vars;
  if (e.that?.length) out.that = e.that;
  if (e.dropped) out.dropped = true;
  if (e.error) out.error = e.error;
  return Object.keys(out).length ? out : undefined;
}
