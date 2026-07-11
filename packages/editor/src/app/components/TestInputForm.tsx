"use client";

import { useState } from "react";
import JsonEditor from "../cel/JsonEditor";
import type { TestInput } from "../meta/types";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

const CODE =
  "rounded-md border border-black/10 dark:border-white/15 overflow-auto octo-code";

/**
 * The form behind "Add test input": a name, the JSON body, and the message variables.
 *
 * Variables are offered because an invoked flow starts no sources, so nothing populates
 * them — a flow reading a header an HTTP source would normally have copied across sees
 * nothing unless it is supplied here. Leaving them out is the most common reason a flow
 * behaves differently under test than in production.
 */
export default function TestInputForm({
  initial,
  onSubmit,
  onCancel,
}: {
  /** The input being edited, or undefined when creating one. */
  initial?: TestInput;
  onSubmit(input: Omit<TestInput, "id">): void;
  onCancel(): void;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [data, setData] = useState(initial?.data ?? "");
  const [vars, setVars] = useState(initial?.vars ?? "");

  const invalid = (text: string) => {
    if (text.trim() === "") return false;
    try {
      JSON.parse(text);
      return false;
    } catch {
      return true;
    }
  };
  const badData = invalid(data);
  const badVars = invalid(vars);
  const canSave = name.trim() !== "" && !badData && !badVars;

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (!canSave) return;
        onSubmit({
          name: name.trim(),
          data: data.trim() || undefined,
          vars: vars.trim() || undefined,
        });
      }}
      className="flex flex-col gap-2 p-3"
    >
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium text-zinc-500">Name</span>
        <input
          autoFocus
          type="text"
          value={name}
          placeholder="happy path"
          onChange={(e) => setName(e.target.value)}
          className={FIELD}
        />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium text-zinc-500">Body (JSON)</span>
        <div className={`${CODE} ${badData ? "border-rose-400/70" : ""}`}>
          <JsonEditor value={data} onChange={setData} placeholder='{ "orderId": 42 }' />
        </div>
      </label>

      <label className="flex flex-col gap-1">
        <span className="flex items-center justify-between text-xs font-medium text-zinc-500">
          Variables (JSON)
          <span className="font-normal text-zinc-400 dark:text-zinc-500">
            what a source would set
          </span>
        </span>
        <div className={`${CODE} ${badVars ? "border-rose-400/70" : ""}`}>
          <JsonEditor value={vars} onChange={setVars} placeholder='{ "x-api-key": "dev" }' />
        </div>
      </label>

      {(badData || badVars) && (
        <p className="text-xs text-rose-500">That isn&apos;t valid JSON.</p>
      )}

      <div className="flex items-center justify-end gap-1.5 pt-1">
        <button
          type="button"
          onClick={onCancel}
          className="rounded-md px-2 py-1 text-xs text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={!canSave}
          className="rounded-md bg-sky-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:opacity-40"
        >
          {initial ? "Save" : "Add"}
        </button>
      </div>
    </form>
  );
}
