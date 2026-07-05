"use client";

import { useState } from "react";
import { Check, Copy, Plug } from "lucide-react";

/**
 * The MCP endpoint panel: shows the public server URL an MCP client (Claude,
 * ChatGPT, …) connects to, with a copy button. Auth happens over OAuth 2.1 — the
 * client discovers the authorization server from the URL and signs in — so
 * there's no key to paste; the URL is all the user needs.
 */
export function McpEndpointTile({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);

  const copy = () => {
    // navigator.clipboard is unavailable on insecure origins; fail quietly so the
    // URL is still selectable/readable in the field.
    navigator.clipboard?.writeText(url).then(
      () => {
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      },
      () => {},
    );
  };

  return (
    <section className="mt-6 rounded-xl border border-black/10 bg-white/40 p-4 dark:border-white/10 dark:bg-zinc-900/30">
      <div className="flex items-center gap-3">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-black/[0.05] text-zinc-600 dark:bg-white/[0.08] dark:text-zinc-300">
          <Plug size={18} />
        </span>
        <div className="min-w-0">
          <h2 className="text-sm font-medium">MCP endpoint</h2>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">
            Connect Claude, ChatGPT, or any MCP client — sign in with OAuth, no
            key required.
          </p>
        </div>
      </div>
      <div className="mt-3 flex items-center gap-2">
        <input
          readOnly
          value={url}
          onFocus={(e) => e.currentTarget.select()}
          aria-label="MCP endpoint URL"
          className="min-w-0 flex-1 rounded-lg border border-black/10 bg-white/60 px-3 py-2 font-mono text-xs text-zinc-700 dark:border-white/10 dark:bg-black/20 dark:text-zinc-200"
        />
        <button
          type="button"
          onClick={copy}
          className="flex shrink-0 items-center gap-1.5 rounded-lg border border-black/10 px-3 py-2 text-xs font-medium text-zinc-600 transition-colors hover:bg-black/[0.03] dark:border-white/10 dark:text-zinc-300 dark:hover:bg-white/[0.04]"
        >
          {copied ? <Check size={14} /> : <Copy size={14} />}
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
    </section>
  );
}
