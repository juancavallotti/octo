"use client";

import { Plus, X } from "lucide-react";
import type { Binding } from "../types";
import { INPUT, MUTED, SECONDARY } from "../ui";

/** NAME = value rows, for env vars and the cluster secrets they read from. */
export default function BindingRows({
  label,
  valueLabel,
  rows,
  onChange,
}: {
  label: string;
  valueLabel: string;
  rows: Binding[];
  onChange(rows: Binding[]): void;
}) {
  const set = (i: number, patch: Partial<Binding>) => onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <span className={MUTED}>{label}</span>
        <button type="button" onClick={() => onChange([...rows, { name: "", value: "" }])} className={SECONDARY} aria-label={`Add ${label}`}>
          <Plus size={12} />
        </button>
      </div>
      {rows.map((r, i) => (
        <div key={i} className="flex items-center gap-1.5">
          <input value={r.name} onChange={(e) => set(i, { name: e.target.value })} placeholder="NAME" aria-label={`${label} name`} className={`${INPUT} w-40 font-mono`} />
          <span className="text-zinc-400">=</span>
          <input value={r.value} onChange={(e) => set(i, { value: e.target.value })} placeholder={valueLabel} aria-label={`${label} value`} className={`${INPUT} flex-1 font-mono`} />
          <button type="button" onClick={() => onChange(rows.filter((_, j) => j !== i))} className="text-zinc-400 hover:text-zinc-700" aria-label="Remove">
            <X size={14} />
          </button>
        </div>
      ))}
    </div>
  );
}
