"use client";

import { Webhook } from "lucide-react";
import type { SourceNode } from "@/app/model/document";
import FlowNode from "./FlowNode";

/**
 * The source node sits at the top of a flow, above the dashed divider. It shows
 * the flow's source (type + connector) read-only for now — a source picker lands
 * with the settings editor.
 */
export default function SourceCard({ source }: { source?: SourceNode }) {
  const icon = (
    <Webhook size={20} className="text-zinc-600 dark:text-zinc-300" />
  );
  const label = source?.type ?? "Source";
  const sublabel = source?.type
    ? source.connector
    : "callable by name";

  return <FlowNode icon={icon} label={label} sublabel={sublabel} />;
}
