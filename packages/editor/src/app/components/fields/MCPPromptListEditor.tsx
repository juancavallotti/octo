"use client";

import { useState } from "react";
import { Plus, X } from "lucide-react";
import ReferenceField from "./ReferenceField";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** One declared argument of an MCP prompt. */
type PromptArg = { name: string; description: string; required: boolean };

/** One MCP prompt: advertised metadata, its arguments, and the template it renders. */
type Prompt = {
  name: string;
  description: string;
  arguments: PromptArg[];
  resource: string;
};

function toPrompt(value: unknown): Prompt {
  const v = (value ?? {}) as Partial<Prompt>;
  return {
    name: v.name ?? "",
    description: v.description ?? "",
    arguments: Array.isArray(v.arguments)
      ? v.arguments.map((a) => ({
          name: a?.name ?? "",
          description: a?.description ?? "",
          required: Boolean(a?.required),
        }))
      : [],
    resource: v.resource ?? "",
  };
}

/**
 * Editor for mcp-router's `prompts` (the `mcp-prompt-list` field type). Each entry
 * advertises a template resource as an MCP prompt: a name, optional description,
 * a list of named arguments (exposed to the template as the body on prompts/get),
 * and the declared template it renders. Prompts hold no sub-flow, so this edits
 * plain data and emits the list on every change. Seeded once from `value`; the
 * parent remounts it per block.
 */
export default function MCPPromptListEditor({
  value,
  onChange,
}: {
  value: unknown;
  onChange: (value: Prompt[]) => void;
}) {
  const [rows, setRows] = useState<Prompt[]>(() =>
    Array.isArray(value) ? value.map(toPrompt) : [],
  );

  function commit(next: Prompt[]) {
    setRows(next);
    onChange(next);
  }

  function patch(i: number, edit: Partial<Prompt>) {
    commit(rows.map((r, j) => (j === i ? { ...r, ...edit } : r)));
  }

  function patchArgs(i: number, args: PromptArg[]) {
    patch(i, { arguments: args });
  }

  return (
    <div className="flex flex-col gap-2">
      {rows.map((row, i) => (
        <div
          key={i}
          className="flex flex-col gap-1.5 rounded-md border border-black/10 dark:border-white/15 p-2"
        >
          <div className="flex items-center gap-1.5">
            <input
              type="text"
              value={row.name}
              placeholder="name"
              onChange={(e) => patch(i, { name: e.target.value })}
              className={INPUT}
            />
            <button
              type="button"
              aria-label="Remove prompt"
              onClick={() => commit(rows.filter((_, j) => j !== i))}
              className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
            >
              <X size={14} />
            </button>
          </div>
          <input
            type="text"
            value={row.description}
            placeholder="description (optional)"
            onChange={(e) => patch(i, { description: e.target.value })}
            className={INPUT}
          />
          <ReferenceField
            spec={{ kind: "template" }}
            value={row.resource}
            required
            onChange={(v) => patch(i, { resource: v === undefined ? "" : String(v) })}
          />
          <ArgumentsEditor
            args={row.arguments}
            onChange={(args) => patchArgs(i, args)}
          />
        </div>
      ))}
      <button
        type="button"
        onClick={() =>
          commit([...rows, { name: "", description: "", arguments: [], resource: "" }])
        }
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add prompt
      </button>
    </div>
  );
}

/** The nested arguments list for one prompt: a name, optional description, and a required flag. */
function ArgumentsEditor({
  args,
  onChange,
}: {
  args: PromptArg[];
  onChange: (args: PromptArg[]) => void;
}) {
  function patch(i: number, edit: Partial<PromptArg>) {
    onChange(args.map((a, j) => (j === i ? { ...a, ...edit } : a)));
  }

  return (
    <div className="flex flex-col gap-1.5 rounded-md border border-dashed border-black/10 dark:border-white/15 p-2">
      <span className="text-xs font-medium text-zinc-500 dark:text-zinc-400">
        Arguments
      </span>
      {args.map((arg, i) => (
        <div key={i} className="flex items-center gap-1.5">
          <input
            type="text"
            value={arg.name}
            placeholder="name"
            onChange={(e) => patch(i, { name: e.target.value })}
            className={INPUT}
          />
          <input
            type="text"
            value={arg.description}
            placeholder="description"
            onChange={(e) => patch(i, { description: e.target.value })}
            className={INPUT}
          />
          <label
            className="flex shrink-0 items-center gap-1 text-xs text-zinc-500 dark:text-zinc-400"
            title="Required"
          >
            <input
              type="checkbox"
              checked={arg.required}
              onChange={(e) => patch(i, { required: e.target.checked })}
              className="h-3.5 w-3.5 accent-sky-500"
            />
            req
          </label>
          <button
            type="button"
            aria-label="Remove argument"
            onClick={() => onChange(args.filter((_, j) => j !== i))}
            className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
          >
            <X size={14} />
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={() => onChange([...args, { name: "", description: "", required: false }])}
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-0.5 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={12} />
        Add argument
      </button>
    </div>
  );
}
