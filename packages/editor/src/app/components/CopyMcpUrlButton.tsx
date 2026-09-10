"use client";

import { useEffect, useRef, useState } from "react";
import { Check, TriangleAlert } from "lucide-react";
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
  // null while idle; true after a copy, false when the clipboard refused — which it
  // does on an insecure origin or a denied permission. Saying so matters more here
  // than elsewhere: the whole point was to get this URL somewhere else, and a button
  // that silently did nothing leaves you pasting a stale one.
  const [copied, setCopied] = useState<boolean | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const copy = async () => {
    let ok = true;
    try {
      await navigator.clipboard.writeText(url);
    } catch {
      ok = false;
    }
    setCopied(ok);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(null), ok ? 1500 : 4000);
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
        {copied === true ? (
          <Check className="h-3.5 w-3.5 text-emerald-500" />
        ) : copied === false ? (
          <TriangleAlert className="h-3.5 w-3.5 text-amber-500" />
        ) : (
          <McpIcon size={14} />
        )}
      </button>

      {/* Above the button, not below: the console header sits at the bottom of the
          window, where there is no room under it. Right-aligned for the same reason
          the panel's other menus are — it is at the end of the bar. */}
      <span
        role="tooltip"
        className={`absolute bottom-full right-0 z-50 mb-1.5 hidden whitespace-nowrap rounded-md border border-black/10 bg-white px-2 py-1 text-[11px] shadow-lg group-hover:block group-focus-within:block dark:border-white/10 dark:bg-zinc-900 ${
          copied === false ? "block select-text" : "pointer-events-none"
        }`}
      >
        <span className="text-zinc-400">
          {copied === true
            ? "Copied"
            : copied === false
              ? "Could not copy — select it here:"
              : "Copy MCP endpoint"}
        </span>{" "}
        <span className="font-mono text-zinc-700 dark:text-zinc-200">
          {url}
        </span>
      </span>
    </span>
  );
}
