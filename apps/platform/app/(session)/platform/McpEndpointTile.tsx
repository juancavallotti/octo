"use client";

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { McpIcon } from "@octo/editor";

/**
 * The MCP panel: the public server URL an MCP client (Claude, ChatGPT, …) connects
 * to, with a copy button. Auth happens over OAuth 2.1 — the client discovers the
 * authorization server from the URL and signs in — so there is no key to paste;
 * the URL is all somebody needs.
 *
 * It sits beside the page title rather than in the tile grid, so it is compact and
 * tinted: this is the one address on the dashboard people come back to copy, and
 * the gradient marks it as the odd one out among the plain navigation tiles.
 *
 * The copy button lives inside the field rather than beside it, which is where a
 * reader now looks for it — and it keeps the address the full width of the card.
 *
 * Name and line sit together beside the icon rather than stacked as three bands.
 * The card shares a row with the page title, so its height is what decides whether
 * that row reads as one thing; two lines against one icon is the compact shape.
 */
export function McpEndpointTile({
  url,
  className,
}: {
  url: string;
  /** Sizing from the caller — the tile itself is layout-agnostic. */
  className?: string;
}) {
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
    <section
      className={`rounded-xl border border-indigo-500/25 bg-gradient-to-br from-indigo-500/[0.12] via-sky-500/[0.06] to-transparent p-2.5 ${className ?? ""}`}
    >
      <div className="flex items-center gap-2.5">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-indigo-600 text-white">
          <McpIcon size={18} />
        </span>
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <h2 className="text-sm font-medium">MCP</h2>
            <span className="rounded-full bg-indigo-500/15 px-1.5 py-0.5 text-[10px] font-medium text-indigo-600 dark:text-indigo-300">
              OAuth — no key
            </span>
          </div>
          <p className="truncate text-xs text-zinc-500 dark:text-zinc-400">
            Author, deploy and test integrations from Claude, ChatGPT, or any MCP client.
          </p>
        </div>
      </div>
      <div className="relative mt-2">
        <input
          readOnly
          value={url}
          onFocus={(e) => e.currentTarget.select()}
          aria-label="MCP endpoint URL"
          className="w-full rounded-lg border border-black/10 bg-white/70 py-1.5 pl-2.5 pr-9 font-mono text-xs text-zinc-700 dark:border-white/10 dark:bg-black/20 dark:text-zinc-200"
        />
        <button
          type="button"
          onClick={copy}
          title={copied ? "Copied" : "Copy address"}
          aria-label={copied ? "Copied" : "Copy address"}
          className="absolute inset-y-0 right-0 flex items-center rounded-r-lg px-2.5 text-zinc-400 transition-colors hover:text-indigo-600 dark:hover:text-indigo-300"
        >
          {copied ? <Check size={14} className="text-emerald-500" /> : <Copy size={14} />}
        </button>
      </div>
    </section>
  );
}
