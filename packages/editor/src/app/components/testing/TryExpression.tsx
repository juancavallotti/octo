"use client";

import { useState } from "react";
import { Loader2, Play } from "lucide-react";
import { useRun } from "../../run/RunContext";
import type { CelSample } from "./sample";

type Verdict =
  | { kind: "true" }
  | { kind: "false" }
  | { kind: "not-boolean"; rendered: string }
  | { kind: "error"; message: string };

/**
 * Evaluate one `that:` expression without running the suite.
 *
 * Worth its own control because a `that:` entry is the only part of a case that can be
 * wrong in a way nothing else catches until a run: the JSON fields are checked as they
 * are typed and the rules check the rest of the file, but an expression is opaque until
 * something compiles it.
 *
 * Two things it is careful about, both to match what dolphin does rather than what CEL
 * allows:
 *
 *   **A non-boolean result is a mistake, not a falsy value.** `body.total` is not an
 *   assertion, and dolphin fails the case rather than reading it as one — a truthy
 *   non-zero number silently passing would be a test that checks nothing. So that is
 *   reported here as the problem it is, not as a result.
 *
 *   **`env` is not bound.** dolphin never loads the config — octo does, in the child
 *   process — so a `that:` expression referencing `env.X` fails there. Binding it here
 *   would let the editor bless an expression the run refuses.
 */
export default function TryExpression({
  expression,
  sample,
}: {
  expression: string;
  /** The last run's message for this case, or null when there has not been one. */
  sample: CelSample | null;
}) {
  const run = useRun();
  const [busy, setBusy] = useState(false);
  const [verdict, setVerdict] = useState<Verdict | null>(null);

  if (!run?.available) return null;

  const blank = expression.trim() === "";

  const evaluate = async () => {
    setBusy(true);
    setVerdict(null);
    try {
      const result = await run.evalCel({
        expression,
        data: sample?.body === undefined ? undefined : JSON.stringify(sample.body),
        vars: sample?.vars === undefined ? undefined : JSON.stringify(sample.vars),
      });
      setVerdict(verdictOf(result));
    } catch (e) {
      setVerdict({ kind: "error", message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex items-center gap-2">
      <button
        type="button"
        disabled={blank || busy}
        onClick={() => void evaluate()}
        title={
          blank
            ? "Write an expression first"
            : sample
              ? "Evaluate against the message the last run produced"
              : "No run yet, so this only checks that it compiles"
        }
        className="flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-zinc-500 hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-white/10"
      >
        {busy ? <Loader2 size={11} className="animate-spin" /> : <Play size={11} />}
        Try
      </button>
      {verdict && <Result verdict={verdict} sampled={!!sample} />}
    </div>
  );
}

function Result({ verdict, sampled }: { verdict: Verdict; sampled: boolean }) {
  if (verdict.kind === "error") {
    return (
      <span className="min-w-0 truncate font-mono text-[11px] text-rose-500">
        {verdict.message}
      </span>
    );
  }
  if (verdict.kind === "not-boolean") {
    return (
      <span className="min-w-0 text-[11px] text-amber-600 dark:text-amber-500">
        Not a true/false expression — it returned{" "}
        <code>{verdict.rendered}</code>, which dolphin fails the case on.
      </span>
    );
  }
  const holds = verdict.kind === "true";
  return (
    <span
      className={`text-[11px] ${
        holds
          ? "text-emerald-600 dark:text-emerald-400"
          : "text-zinc-500 dark:text-zinc-400"
      }`}
    >
      {holds ? "true" : "false"}
      {!sampled && " — against an empty message"}
    </span>
  );
}

function verdictOf(result: { ok: boolean; result?: unknown; error?: string }): Verdict {
  if (!result.ok || result.error) {
    return { kind: "error", message: result.error ?? "The expression could not be run." };
  }
  if (typeof result.result !== "boolean") {
    return { kind: "not-boolean", rendered: JSON.stringify(result.result) ?? "nothing" };
  }
  return { kind: result.result ? "true" : "false" };
}
