"use client";

/**
 * A captured payload, as JSON.
 *
 * Three states have to stay distinguishable, because they render almost
 * identically and mean completely different things:
 *
 *  - **absent** — the runtime captured nothing, or capture is off
 *  - **empty** — the flow really did carry `{}` or `[]`
 *  - **loading** — it exists and is on its way
 *
 * A trace is read to find out what a flow actually carried, so an empty document
 * shown where a missing one belongs is not a cosmetic error.
 */

/** Beyond this the document is cut, with a line saying so. A traced body can be
 * megabytes, and the browser is not the place to find that out. */
const MAX_CHARS = 20_000;

export default function Payload({
  label,
  value,
  loading,
  absentNote,
}: {
  label: string;
  /** The captured value, or null when nothing was captured. */
  value: unknown;
  loading?: boolean;
  /** What to say when there is nothing — the reason differs by payload. */
  absentNote: string;
}) {
  const rendered = value === null || value === undefined ? null : render(value);

  return (
    <section className="min-w-0">
      <h4 className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-zinc-400">
        {label}
      </h4>
      {loading ? (
        <p className="text-xs text-zinc-400">Loading…</p>
      ) : rendered === null ? (
        <p className="text-xs text-zinc-400">{absentNote}</p>
      ) : (
        <pre className="max-h-64 overflow-auto rounded-md bg-black/[0.04] p-2 font-mono text-[11px] leading-relaxed dark:bg-white/[0.06]">
          {rendered}
        </pre>
      )}
    </section>
  );
}

function render(value: unknown): string {
  let text: string;
  try {
    text = JSON.stringify(value, null, 2) ?? String(value);
  } catch {
    // A payload that cannot be stringified — a cycle, a BigInt — is still worth
    // saying something about rather than blanking the panel.
    return "(this payload could not be rendered)";
  }
  if (text.length <= MAX_CHARS) return text;
  return `${text.slice(0, MAX_CHARS)}\n\n… cut here: ${text.length - MAX_CHARS} more characters`;
}
