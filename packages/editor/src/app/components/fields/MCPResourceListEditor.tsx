"use client";

import { useState } from "react";
import { Braces, Plus, X } from "lucide-react";
import ReferenceField from "./ReferenceField";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * One MCP resource: the uri clients read it by, advertised metadata, and its
 * template. Exactly one of `uri` and `uriTemplate` is set — a uri is one fixed
 * document, a uriTemplate ("contacts://contact/{id}") a family of them, and the
 * two are advertised on different MCP methods.
 */
type Resource = {
  uri: string;
  uriTemplate: string;
  name: string;
  description: string;
  mimeType: string;
  resource: string;
};

function toResource(value: unknown): Resource {
  const v = (value ?? {}) as Partial<Resource>;
  return {
    uri: v.uri ?? "",
    uriTemplate: v.uriTemplate ?? "",
    name: v.name ?? "",
    description: v.description ?? "",
    mimeType: v.mimeType ?? "",
    resource: v.resource ?? "",
  };
}

/** A row addresses a family of documents once it has a uriTemplate. */
function isTemplated(row: Resource): boolean {
  return row.uriTemplate !== "";
}

/**
 * Editor for mcp-router's `resources` (the `mcp-resource-list` field type). Each
 * entry advertises a template resource as an MCP resource: a stable uri, name,
 * optional description/mimeType, and the declared template it serves. Resources
 * hold no sub-flow, so this edits plain data and emits the list on every change.
 * Seeded once from `value`; the parent remounts it per block.
 */
export default function MCPResourceListEditor({
  value,
  onChange,
}: {
  value: unknown;
  onChange: (value: Resource[]) => void;
}) {
  const [rows, setRows] = useState<Resource[]>(() =>
    Array.isArray(value) ? value.map(toResource) : [],
  );

  function commit(next: Resource[]) {
    setRows(next);
    onChange(next);
  }

  function patch(i: number, edit: Partial<Resource>) {
    commit(rows.map((r, j) => (j === i ? { ...r, ...edit } : r)));
  }

  return (
    <div className="flex flex-col gap-2">
      {rows.map((row, i) => (
        <div
          key={i}
          className="flex flex-col gap-1.5 rounded-md border border-black/10 dark:border-white/15 p-2"
        >
          <div className="flex items-center gap-1.5">
            {/*
              The two address fields are mutually exclusive, so only the one in
              use is shown: typing a uriTemplate hides the uri and vice versa.
              The runtime rejects a resource declaring both, and an editor that
              let you type both would only be a way to build a broken flow.
            */}
            {isTemplated(row) ? (
              <input
                type="text"
                value={row.uriTemplate}
                placeholder="uriTemplate (e.g. octo://city/{city})"
                onChange={(e) => patch(i, { uriTemplate: e.target.value })}
                className={INPUT}
              />
            ) : (
              <input
                type="text"
                value={row.uri}
                placeholder="uri (e.g. octo://guide)"
                onChange={(e) => patch(i, { uri: e.target.value })}
                className={INPUT}
              />
            )}
            <button
              type="button"
              aria-label={isTemplated(row) ? "Use a fixed uri" : "Use a uri template"}
              title={isTemplated(row) ? "Use a fixed uri" : "Use a uri template"}
              onClick={() =>
                patch(i, isTemplated(row) ? { uriTemplate: "" } : { uri: "", uriTemplate: row.uri })
              }
              className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-zinc-600 dark:hover:text-zinc-300"
            >
              <Braces size={14} />
            </button>
            <button
              type="button"
              aria-label="Remove resource"
              onClick={() => commit(rows.filter((_, j) => j !== i))}
              className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
            >
              <X size={14} />
            </button>
          </div>
          <input
            type="text"
            value={row.name}
            placeholder="name"
            onChange={(e) => patch(i, { name: e.target.value })}
            className={INPUT}
          />
          <input
            type="text"
            value={row.description}
            placeholder="description (optional)"
            onChange={(e) => patch(i, { description: e.target.value })}
            className={INPUT}
          />
          <input
            type="text"
            value={row.mimeType}
            placeholder="mimeType (optional, default text/plain)"
            onChange={(e) => patch(i, { mimeType: e.target.value })}
            className={INPUT}
          />
          <ReferenceField
            spec={{ kind: "template" }}
            value={row.resource}
            required
            onChange={(v) => patch(i, { resource: v === undefined ? "" : String(v) })}
          />
        </div>
      ))}
      <button
        type="button"
        onClick={() =>
          commit([
            ...rows,
            { uri: "", uriTemplate: "", name: "", description: "", mimeType: "", resource: "" },
          ])
        }
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add resource
      </button>
    </div>
  );
}
