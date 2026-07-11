"use client";

import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import CelEditor from "../cel/CelEditor";
import JsonEditor from "../cel/JsonEditor";
import { isValidCase, type BlockMock, type MockCase } from "../meta/types";

const CODE = "rounded-md border border-black/10 dark:border-white/15 overflow-auto octo-code";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** The three things a block can do, and which field each one is. */
type Outcome = "body" | "error" | "drop";

function outcomeOf(c: MockCase): Outcome {
  if (c.error !== undefined) return "error";
  if (c.drop) return "drop";
  return "body";
}

/**
 * Switch a case to a different outcome, dropping the fields the old one owned.
 *
 * They cannot be kept "just in case the user switches back": the runtime rejects a case
 * that sets two outcomes, so a stashed `error` left behind a `body` would be a spec the
 * runner refuses — and a mock is exactly the thing you fiddle with until it is right.
 */
function withOutcome(c: MockCase, outcome: Outcome): MockCase {
  const { when } = c;
  if (outcome === "drop") return { when, drop: true };
  if (outcome === "error") return { when, error: c.error ?? "" };
  return { when, body: c.body ?? "" };
}

function badJson(text: string | undefined): boolean {
  if (!text || text.trim() === "") return false;
  try {
    JSON.parse(text);
    return false;
  } catch {
    return true;
  }
}

/**
 * The form behind "Mock this block": the cases the block answers with instead of running.
 *
 * It mirrors TestInputForm — an inline panel, not a modal — because it is reached the same
 * way, from a button on the thing it configures.
 *
 * The form holds the user to the runtime's one rule that the type cannot: a case does
 * exactly ONE thing. A block either returns a message, fails, or drops it, so the outcome
 * is a three-way choice rather than three fields you could fill in at once. Letting them
 * be filled in at once and rejecting it later would be a worse way to teach the same rule.
 */
export default function MockForm({
  initial,
  onSubmit,
  onRemove,
  onCancel,
}: {
  /** The mock being edited, or undefined when placing one. */
  initial?: BlockMock;
  onSubmit(mock: Omit<BlockMock, "address">): void;
  /** Remove the mock entirely. Absent when there is nothing to remove yet. */
  onRemove?(): void;
  onCancel(): void;
}) {
  const [cases, setCases] = useState<MockCase[]>(initial?.cases ?? []);
  const [fallback, setFallback] = useState<MockCase | undefined>(initial?.default);
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);

  const editCase = (i: number, next: MockCase) =>
    setCases((prev) => prev.map((c, at) => (at === i ? next : c)));

  const invalid =
    cases.some((c) => badJson(c.body) || badJson(c.vars) || !c.when?.trim()) ||
    badJson(fallback?.body) ||
    badJson(fallback?.vars);

  // The runner needs something to answer with. A mock that says nothing would stand in for
  // the block and then have nothing to return, which fails it — never what anyone meant.
  const empty = cases.length === 0 && !fallback;

  /** One case's editor: its condition (absent on the default) and its single outcome. */
  const caseFields = (c: MockCase, onChange: (next: MockCase) => void, isDefault: boolean) => {
    const outcome = outcomeOf(c);
    return (
      <div className="flex flex-col gap-1.5">
        {!isDefault && (
          <div className={`${CODE} ${!c.when?.trim() ? "border-amber-400/70" : ""}`}>
            <CelEditor
              value={c.when ?? ""}
              onChange={(when) => onChange({ ...c, when })}
              placeholder="body.amount > 100"
              minHeight={28}
            />
          </div>
        )}

        <div className="flex gap-1">
          {(["body", "error", "drop"] as Outcome[]).map((o) => (
            <button
              key={o}
              type="button"
              onClick={() => onChange(withOutcome(c, o))}
              className={`rounded-md px-2 py-0.5 text-xs capitalize ${
                outcome === o
                  ? "bg-sky-600 text-white"
                  : "text-zinc-500 hover:bg-black/[0.05] dark:hover:bg-white/10"
              }`}
            >
              {o}
            </button>
          ))}
        </div>

        {outcome === "body" && (
          <>
            <div className={`${CODE} ${badJson(c.body) ? "border-rose-400/70" : ""}`}>
              <JsonEditor
                value={c.body ?? ""}
                onChange={(body) => onChange({ ...c, body })}
                placeholder='{ "charged": true }'
              />
            </div>
            <div className={`${CODE} ${badJson(c.vars) ? "border-rose-400/70" : ""}`}>
              <JsonEditor
                value={c.vars ?? ""}
                onChange={(vars) => onChange({ ...c, vars: vars || undefined })}
                placeholder='variables, e.g. { "status": "200" }'
              />
            </div>
          </>
        )}

        {outcome === "error" && (
          <input
            type="text"
            value={c.error ?? ""}
            placeholder="card declined"
            onChange={(e) => onChange({ ...c, error: e.target.value })}
            className={FIELD}
          />
        )}

        {outcome === "drop" && (
          <p className="text-xs text-zinc-500">
            The message is filtered out, as a filter block would.
          </p>
        )}
      </div>
    );
  };

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (invalid || empty) return;
        onSubmit({
          enabled,
          cases: cases.filter(isValidCase),
          ...(fallback ? { default: fallback } : {}),
        });
      }}
      className="flex max-h-[26rem] flex-col gap-3 overflow-auto p-3"
    >
      <label className="flex items-center gap-2 text-xs font-medium text-zinc-500">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        Stand in for this block when the flow runs
      </label>

      {cases.map((c, i) => (
        <div key={i} className="flex flex-col gap-1.5 rounded-lg border border-black/10 p-2 dark:border-white/10">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-zinc-500">When…</span>
            <button
              type="button"
              aria-label="Remove case"
              onClick={() => setCases((prev) => prev.filter((_, at) => at !== i))}
              className="text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          </div>
          {caseFields(c, (next) => editCase(i, next), false)}
        </div>
      ))}

      <button
        type="button"
        onClick={() => setCases((prev) => [...prev, { when: "", body: "" }])}
        className="flex items-center gap-1 self-start text-xs text-sky-600 hover:underline dark:text-sky-400"
      >
        <Plus size={13} /> Add case
      </button>

      <div className="flex flex-col gap-1.5 rounded-lg border border-black/10 p-2 dark:border-white/10">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-zinc-500">Otherwise…</span>
          {fallback ? (
            <button
              type="button"
              aria-label="Remove default"
              onClick={() => setFallback(undefined)}
              className="text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          ) : (
            <button
              type="button"
              onClick={() => setFallback({ body: "" })}
              className="text-xs text-sky-600 hover:underline dark:text-sky-400"
            >
              Add
            </button>
          )}
        </div>
        {fallback ? (
          caseFields(fallback, setFallback, true)
        ) : (
          // The one thing everybody gets wrong. Say it where the mistake is made.
          <p className="text-xs text-amber-600 dark:text-amber-500">
            Without this, a message matching no case <strong>fails</strong> the block — a
            mock replaces the block, so there is no real one left to fall through to.
          </p>
        )}
      </div>

      {empty && (
        <p className="text-xs text-rose-500">
          A mock needs at least one case, or a default.
        </p>
      )}

      <div className="flex items-center justify-between gap-1.5 pt-1">
        {onRemove ? (
          <button
            type="button"
            onClick={onRemove}
            className="rounded-md px-2 py-1 text-xs text-red-500 hover:bg-red-500/10"
          >
            Remove mock
          </button>
        ) : (
          <span />
        )}
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md px-2 py-1 text-xs text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={invalid || empty}
            className="rounded-md bg-sky-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:opacity-40"
          >
            Save
          </button>
        </div>
      </div>
    </form>
  );
}
