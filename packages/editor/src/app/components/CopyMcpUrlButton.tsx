"use client";

import { useEffect, useRef, useState } from "react";
import { Check } from "lucide-react";
import { McpIcon } from "../schema/mcp-icon";

/**
 * Copies the MCP endpoint this editor is served next to.
 *
 * The endpoint is the whole reason an agent can drive the editor, and it is
 * otherwise invisible from inside it. It used to hide at the bottom of the desktop
 * shell's folder menu, where only one of the three deployments could reach it —
 * every deployment serves one.
 *
 * It wears the MCP mark rather than a copy glyph: a bare pair of pages next to the
 * trash bin says "copy" without ever saying copy *what*. Hovering shows the URL,
 * which is the other half of the answer — you usually want to know where the thing
 * is pointing before you paste it into an agent's config.
 *
 * The URL comes from the host (`consoleActions`), because only the host knows it:
 * the platform's is configured (it may sit behind a proxy), the desktop shell's
 * comes from the shell, and a browser's is simply its own origin.
 */
export default function CopyMcpUrlButton({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const copy = async () => {
    await navigator.clipboard.writeText(url);
    setCopied(true);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1500);
  };

  return (
    <span className="group relative">
      <button
        type="button"
        // The console header is a click-to-collapse strip; this is a control on it.
        onClick={(e) => {
          e.stopPropagation();
          void copy();
        }}
        aria-label="Copy MCP endpoint URL"
        className="flex rounded p-1 text-zinc-500 hover:bg-black/5 dark:hover:bg-white/10"
      >
        {copied ? (
          <Check className="h-3.5 w-3.5 text-emerald-500" />
        ) : (
          <McpIcon size={14} />
        )}
      </button>

      {/* Above the button, not below: the console header sits at the bottom of the
          window, where there is no room under it. Right-aligned for the same reason
          the panel's other menus are — it is at the end of the bar. */}
      <span
        role="tooltip"
        className="pointer-events-none absolute bottom-full right-0 z-50 mb-1.5 hidden whitespace-nowrap rounded-md border border-black/10 bg-white px-2 py-1 text-[11px] shadow-lg group-hover:block group-focus-within:block dark:border-white/10 dark:bg-zinc-900"
      >
        <span className="text-zinc-400">
          {copied ? "Copied" : "Copy MCP endpoint"}
        </span>{" "}
        <span className="font-mono text-zinc-700 dark:text-zinc-200">{url}</span>
      </span>
    </span>
  );
}
