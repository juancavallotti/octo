"use client";

import { useState } from "react";
import { Check, Copy } from "lucide-react";

/**
 * A one-line shell command shown in a code box with a copy-to-clipboard button —
 * used on the welcome page for the standalone Docker quickstart.
 */
export default function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex w-full items-center gap-2 rounded-lg border border-black/10 bg-black/[0.03] px-3 py-2 dark:border-white/10 dark:bg-white/[0.04]">
      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-left font-mono text-xs text-zinc-700 dark:text-zinc-300">
        {command}
      </code>
      <button
        type="button"
        aria-label="Copy command"
        onClick={() => {
          navigator.clipboard?.writeText(command).then(
            () => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1200);
            },
            () => {},
          );
        }}
        className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-600 dark:hover:bg-white/10 dark:hover:text-zinc-300"
      >
        {copied ? (
          <Check size={13} className="text-emerald-500" />
        ) : (
          <Copy size={13} />
        )}
      </button>
    </div>
  );
}
