"use client";

import {
  PanelBottom,
  PanelBottomInactive,
  PanelLeft,
  PanelLeftInactive,
  PanelRight,
  PanelRightInactive,
} from "lucide-react";
import { useLayout } from "../state/layout";
import { useConsoleCollapsed } from "../run/console";
import { useRun } from "../run/RunContext";
import { useEditorState } from "../state/editorState";

/**
 * The three panel toggles, top-right: palette, console, settings — the same idiom,
 * and the same corner, as VS Code's. A hidden panel is drawn with its region
 * dashed, so the row says what the workspace looks like without being read.
 *
 * Only the toggles for panels the current view actually has are shown. The palette
 * and the settings panel belong to the canvas; the YAML, Resources and Testing tabs
 * lay themselves out. The console is shown whenever there is a runner behind it,
 * which is the same condition that decides whether it renders at all.
 */
export default function LayoutToggles() {
  const layout = useLayout();
  const { state } = useEditorState();
  const run = useRun();
  const bottom = useConsoleCollapsed();

  if (!layout) return null;

  const canvas = state.viewMode === "canvas";
  const hasConsole = Boolean(run?.available);
  if (!canvas && !hasConsole) return null;

  const button =
    "rounded-md p-1 text-zinc-500 transition-colors hover:bg-black/[0.05] hover:text-zinc-800 dark:text-zinc-400 dark:hover:bg-white/[0.08] dark:hover:text-zinc-100";

  return (
    <div className="flex items-center gap-0.5">
      {canvas && (
        <button
          type="button"
          aria-label={layout.sidebar ? "Hide components" : "Show components"}
          title={layout.sidebar ? "Hide components" : "Show components"}
          aria-pressed={layout.sidebar}
          onClick={layout.toggleSidebar}
          className={button}
        >
          {layout.sidebar ? (
            <PanelLeft size={16} />
          ) : (
            <PanelLeftInactive size={16} />
          )}
        </button>
      )}

      {hasConsole && (
        <button
          type="button"
          aria-label={bottom.collapsed ? "Show console" : "Hide console"}
          title={bottom.collapsed ? "Show console" : "Hide console"}
          aria-pressed={!bottom.collapsed}
          onClick={bottom.toggle}
          className={button}
        >
          {bottom.collapsed ? (
            <PanelBottomInactive size={16} />
          ) : (
            <PanelBottom size={16} />
          )}
        </button>
      )}

      {canvas && (
        <button
          type="button"
          aria-label={layout.settings ? "Hide settings" : "Show settings"}
          title={layout.settings ? "Hide settings" : "Show settings"}
          aria-pressed={layout.settings}
          onClick={layout.toggleSettings}
          className={button}
        >
          {layout.settings ? (
            <PanelRight size={16} />
          ) : (
            <PanelRightInactive size={16} />
          )}
        </button>
      )}
    </div>
  );
}
