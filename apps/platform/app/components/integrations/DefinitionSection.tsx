"use client";

import { createElement, useMemo } from "react";
import { FileKey2, Files, Plug, Workflow, Zap, type LucideIcon } from "lucide-react";
import { fromDefinitionYaml } from "@octo/editor";

/**
 * Compact, at-a-glance stats for an integration's definition, all parsed straight
 * from the YAML (never a separate SQL read) so the numbers match the document
 * exactly: flows, sources, connectors, and the declared .env files and template
 * resources from the runtime's top-level `resources:` section. Rendered as inline
 * icon chips so the card stays short.
 */

interface Stat {
  label: string;
  value: number;
  icon: LucideIcon;
}

function StatChip({ label, value, icon }: Stat) {
  return (
    <span
      title={label}
      className="inline-flex items-center gap-1 rounded-md bg-black/[0.04] px-2 py-1 text-xs dark:bg-white/[0.06]"
    >
      {createElement(icon, { size: 13, className: "shrink-0 text-zinc-400" })}
      <span className="font-medium tabular-nums">{value}</span>
      <span className="text-zinc-500">{label}</span>
    </span>
  );
}

export default function DefinitionSection({ definition }: { definition: string }) {
  // Everything comes from the parsed document; tolerate YAML we can't parse.
  const stats = useMemo<Stat[] | null>(() => {
    try {
      const doc = fromDefinitionYaml(definition);
      return [
        { label: "Flows", value: doc.flows.length, icon: Workflow },
        {
          label: "Sources",
          value: doc.flows.filter((f) => f.source).length,
          icon: Zap,
        },
        { label: "Connectors", value: doc.connectors.length, icon: Plug },
        {
          label: "Env files",
          value: doc.resources?.env?.length ?? 0,
          icon: FileKey2,
        },
        {
          label: "Resources",
          value: doc.resources?.templates?.length ?? 0,
          icon: Files,
        },
      ];
    } catch {
      return null;
    }
  }, [definition]);

  if (!stats) {
    return (
      <p className="text-sm text-zinc-400">Definition could not be parsed.</p>
    );
  }

  return (
    <div className="flex flex-wrap gap-1.5">
      {stats.map((s) => (
        <StatChip key={s.label} {...s} />
      ))}
    </div>
  );
}
