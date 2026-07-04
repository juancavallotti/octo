"use client";

import { useState } from "react";
import { Plus, X } from "lucide-react";
import ReferenceField from "./ReferenceField";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** One skill: a name/description advertised to the model and the template resource it loads. */
type Skill = { name: string; description: string; resource: string };

function toSkill(value: unknown): Skill {
  const v = (value ?? {}) as Partial<Skill>;
  return {
    name: v.name ?? "",
    description: v.description ?? "",
    resource: v.resource ?? "",
  };
}

/**
 * Editor for ai-agent's `skills` (the `skill-list` field type). Each skill binds a
 * name + description (shown to the model up front) to a declared template resource
 * that the implicit load_skill tool renders on demand. Skills hold no sub-flow, so
 * this edits plain data and emits the list on every change. Seeded once from
 * `value`; the parent remounts it per block so stale rows never leak across
 * selections.
 */
export default function SkillListEditor({
  value,
  onChange,
}: {
  value: unknown;
  onChange: (value: Skill[]) => void;
}) {
  const [skills, setSkills] = useState<Skill[]>(() =>
    Array.isArray(value) ? value.map(toSkill) : [],
  );

  function commit(next: Skill[]) {
    setSkills(next);
    onChange(next);
  }

  function patch(i: number, edit: Partial<Skill>) {
    commit(skills.map((s, j) => (j === i ? { ...s, ...edit } : s)));
  }

  return (
    <div className="flex flex-col gap-2">
      {skills.map((skill, i) => (
        <div
          key={i}
          className="flex flex-col gap-1.5 rounded-md border border-black/10 dark:border-white/15 p-2"
        >
          <div className="flex items-center gap-1.5">
            <input
              type="text"
              value={skill.name}
              placeholder="name"
              onChange={(e) => patch(i, { name: e.target.value })}
              className={INPUT}
            />
            <button
              type="button"
              aria-label="Remove skill"
              onClick={() => commit(skills.filter((_, j) => j !== i))}
              className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
            >
              <X size={14} />
            </button>
          </div>
          <input
            type="text"
            value={skill.description}
            placeholder="description (shown to the model)"
            onChange={(e) => patch(i, { description: e.target.value })}
            className={INPUT}
          />
          <ReferenceField
            spec={{ kind: "template" }}
            value={skill.resource}
            required
            onChange={(v) => patch(i, { resource: v === undefined ? "" : String(v) })}
          />
        </div>
      ))}
      <button
        type="button"
        onClick={() => commit([...skills, { name: "", description: "", resource: "" }])}
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add skill
      </button>
    </div>
  );
}
