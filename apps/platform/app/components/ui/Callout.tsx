import { Info, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

/**
 * A short aside the reader has to take in before acting: a consequence of the
 * setting they are about to turn on, or a caveat about the data in front of them.
 *
 * The tone is carried by the tint and the icon, and the words are left in the
 * ordinary text colour. Coloured prose is harder to read than plain prose, and a
 * paragraph of amber reads as an alarm however mild the sentence is — which
 * spends attention this is only trying to draw.
 *
 * `flush` is for a callout that spans a panel edge to edge, between a header and
 * what it introduces; the default is a rounded box that sits in a column of
 * content.
 */
export default function Callout({
  tone = "warn",
  flush = false,
  children,
}: {
  tone?: "warn" | "note";
  flush?: boolean;
  children: ReactNode;
}) {
  const Icon = tone === "warn" ? TriangleAlert : Info;
  return (
    <div
      className={`flex gap-2 text-xs ${tones[tone].container} ${
        flush ? "border-b px-4 py-2.5" : "rounded-md border px-3 py-2.5"
      }`}
    >
      <Icon size={14} className={`mt-px shrink-0 ${tones[tone].icon}`} />
      <div className="space-y-1.5 text-zinc-700 dark:text-zinc-300">{children}</div>
    </div>
  );
}

const tones = {
  warn: {
    container: "border-amber-500/25 bg-amber-500/5",
    icon: "text-amber-600 dark:text-amber-500",
  },
  note: {
    container: "border-zinc-400/25 bg-zinc-500/5",
    icon: "text-zinc-500",
  },
} as const;
