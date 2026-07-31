"use client";

import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import CelEditor from "../../cel/CelEditor";
import type { MessageExpect } from "../../suite/types";
import JsonValueField from "./JsonValueField";
import TryExpression from "./TryExpression";
import type { CelSample } from "./sample";

/**
 * What a message should look like: the body exactly, the variables it must at least
 * carry, and the CEL that must hold over it.
 *
 * One component for every place the format asks this — a case's expectation, and what a
 * spied block saw going in and coming out — because dolphin matches all of them with the
 * same code. Two forms would eventually disagree about the asymmetry below, which is the
 * one thing about this format nobody guesses right.
 *
 * `body` is an exact deep-equal and `vars` is a subset, and the labels say so rather than
 * leaving it to a doc page. The body is the flow's answer, and a test earns its keep by
 * failing when a new field appears in it; variables are scratch space the engine and the
 * blocks add to, so pinning the whole map would break on every unrelated change.
 *
 * `that` is local state: a row just added says nothing, and an expression that says
 * nothing is not one dolphin could compile, so the file would drop it and the row would
 * vanish from under the cursor.
 */
export default function MessageExpectFields({
  value,
  onChange,
  sample = null,
  bodyPlaceholder = '{ "charged": true }',
}: {
  value: MessageExpect | undefined;
  /** Called with undefined when the matcher no longer asserts anything. */
  onChange: (next: MessageExpect | undefined) => void;
  /** The message the last run produced here, so an expression can be tried against it. */
  sample?: CelSample | null;
  bodyPlaceholder?: string;
}) {
  const [that, setThat] = useState<string[]>(() => value?.that ?? []);

  const write = (next: MessageExpect) => onChange(clean(next));

  const writeThat = (next: string[]) => {
    setThat(next);
    write({ ...value, that: next.filter((e) => e.trim() !== "") });
  };

  return (
    <>
      <JsonValueField
        label="Body"
        hint="exact — the whole body, deep-equal"
        value={value?.body}
        placeholder={bodyPlaceholder}
        minHeight={48}
        onChange={(body) => write({ ...value, body })}
      />
      <JsonValueField
        label="Variables"
        hint="subset — only the ones named here"
        value={value?.vars}
        objectOnly
        placeholder='{ "status": "200" }'
        minHeight={48}
        onChange={(vars) => write({ ...value, vars: vars as Record<string, unknown> })}
      />

      <div className="flex flex-col gap-1">
        <span className="flex items-baseline justify-between text-xs font-medium text-zinc-500">
          That
          <span className="font-normal text-zinc-400 dark:text-zinc-500">
            CEL over the message; each must be true
          </span>
        </span>
        {that.map((expression, i) => (
          <div key={i} className="flex flex-col gap-0.5">
            <div className="flex items-center gap-1">
              <div className="min-w-0 flex-1">
                <CelEditor
                  value={expression}
                  onChange={(next) => writeThat(that.map((e, at) => (at === i ? next : e)))}
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
            <TryExpression expression={expression} sample={sample} />
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
  );
}

/** Drop what says nothing, and the matcher itself when nothing is left. */
function clean(m: MessageExpect): MessageExpect | undefined {
  const out: MessageExpect = {};
  if (m.body !== undefined) out.body = m.body;
  if (m.vars && Object.keys(m.vars).length) out.vars = m.vars;
  if (m.that?.length) out.that = m.that;
  return Object.keys(out).length ? out : undefined;
}
