"use client";

import { FolderOpen } from "lucide-react";
import { BAR_BUTTON, useEditorState } from "@octo/editor";

/**
 * Which document is open, in the platform's document bar — the same spot and the
 * same shape as the standalone's file switcher, minus the switching.
 *
 * A platform integration is one document today, so there is nothing to pick
 * between and this is a label rather than a menu. It is built like the switcher
 * anyway, because that is what it becomes when an integration can hold several
 * flow files.
 */
export default function IntegrationNameChip() {
  const { state } = useEditorState();
  const name = state.integration.name.trim();

  return (
    <span
      className={`${BAR_BUTTON} pointer-events-none ${name ? "" : "text-zinc-400 dark:text-zinc-500"}`}
    >
      <FolderOpen size={15} className="shrink-0 text-zinc-400" />
      <span className="max-w-[16rem] truncate">
        {name || "untitled-integration"}
      </span>
    </span>
  );
}
