"use client";

import { useEffect, useState } from "react";
import { FlaskConical, Play, X } from "lucide-react";
import CelEditor from "./CelEditor";
import { useRun } from "../run/RunContext";
import type { CelEvalResult } from "../run/transport";

/**
 * A modal for trying a CEL expression against an ad-hoc input, without running a
 * flow. The expression uses the same highlighted, autocompleting CelEditor; the
 * body/vars/env inputs are JSON, matching what a message expression sees. Evaluation
 * goes through useRun().evalCel (→ `octo eval`). Modeled on the app's DeployModal
 * overlay pattern. `templateResource()` and a real integration's resolved env are
 * not available here — pass values via the env box.
 */

const JSON_INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1.5 text-xs font-mono outline-none focus:border-black/30 dark:focus:border-white/30 resize-y";

/** Parse an optional JSON object of string values (the env map), or throw a friendly error. */
function parseEnv(text: string): Record<string, string> | undefined {
  if (text.trim() === "") return undefined;
  let obj: unknown;
  try {
    obj = JSON.parse(text);
  } catch {
    throw new Error("env must be valid JSON");
  }
  if (obj === null || typeof obj !== "object" || Array.isArray(obj)) {
    throw new Error("env must be a JSON object");
  }
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(obj)) out[k] = String(v);
  return out;
}

/** Validate that optional text is a JSON value (bound to body/vars), or throw. */
function checkJson(text: string, label: string): void {
  if (text.trim() === "") return;
  try {
    JSON.parse(text);
  } catch {
    throw new Error(`${label} must be valid JSON`);
  }
}

export default function CelTesterModal({ onClose }: { onClose: () => void }) {
  const run = useRun();
  const [expression, setExpression] = useState("");
  const [body, setBody] = useState("");
  const [vars, setVars] = useState("");
  const [env, setEnv] = useState("");
  const [result, setResult] = useState<CelEvalResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Close on Escape, mirroring the editor's other overlays. CelEditor stops
  // propagation while its completion menu is open, so the first Escape dismisses
  // that menu rather than the modal.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !busy) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onClose]);

  const canRun = !busy && expression.trim() !== "" && !!run?.available;

  async function evaluate() {
    if (!run || !canRun) return;
    setError(null);
    setResult(null);
    let envMap: Record<string, string> | undefined;
    try {
      checkJson(body, "body");
      checkJson(vars, "vars");
      envMap = parseEnv(env);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      return;
    }
    setBusy(true);
    try {
      const r = await run.evalCel({
        expression,
        data: body.trim() === "" ? undefined : body,
        vars: vars.trim() === "" ? undefined : vars,
        env: envMap,
      });
      setResult(r);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  // Ctrl/Cmd+Enter runs from anywhere in the modal.
  function onModalKeyDown(e: React.KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void evaluate();
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="CEL expression tester"
      onMouseDown={() => !busy && onClose()}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm"
    >
      <div
        onMouseDown={(e) => e.stopPropagation()}
        onKeyDown={onModalKeyDown}
        className="flex w-full max-w-lg flex-col overflow-hidden rounded-xl border border-black/10 bg-white shadow-xl dark:border-white/10 dark:bg-zinc-900"
      >
        <header className="flex items-center gap-2 border-b border-black/10 px-4 py-3 dark:border-white/10">
          <FlaskConical size={16} className="text-sky-500" />
          <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">
            CEL tester
          </h3>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="rounded p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 dark:hover:bg-white/[0.08] dark:hover:text-zinc-200"
          >
            <X size={16} />
          </button>
        </header>

        <div className="flex max-h-[70vh] flex-col gap-4 overflow-y-auto px-4 py-4">
          <label className="flex flex-col gap-1">
            <span className="text-xs font-medium text-zinc-500">Expression</span>
            <CelEditor
              value={expression}
              onChange={setExpression}
              minHeight={56}
            />
          </label>

          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1">
              <span className="text-xs font-medium text-zinc-500">
                body (JSON)
              </span>
              <textarea
                rows={4}
                value={body}
                onChange={(e) => setBody(e.target.value)}
                placeholder={'{ "id": 1 }'}
                className={JSON_INPUT}
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-xs font-medium text-zinc-500">
                vars (JSON)
              </span>
              <textarea
                rows={4}
                value={vars}
                onChange={(e) => setVars(e.target.value)}
                placeholder={'{ "userId": "u_1" }'}
                className={JSON_INPUT}
              />
            </label>
          </div>

          <label className="flex flex-col gap-1">
            <span className="text-xs font-medium text-zinc-500">
              env (JSON object of strings)
            </span>
            <textarea
              rows={2}
              value={env}
              onChange={(e) => setEnv(e.target.value)}
              placeholder={'{ "API_BASE_URL": "https://api.example.com" }'}
              className={JSON_INPUT}
            />
          </label>

          <ResultPanel result={result} error={error} />
        </div>

        <footer className="flex items-center justify-between gap-2 border-t border-black/10 px-4 py-3 dark:border-white/10">
          <span className="text-[11px] text-zinc-400">
            {run?.available
              ? "⌘/Ctrl + Enter to run"
              : "Runner unavailable — start the dev runner to evaluate."}
          </span>
          <button
            type="button"
            onClick={() => void evaluate()}
            disabled={!canRun}
            className="flex items-center gap-1.5 rounded-md bg-sky-500 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-600 disabled:opacity-50"
          >
            <Play size={14} />
            {busy ? "Running…" : "Run"}
          </button>
        </footer>
      </div>
    </div>
  );
}

/** Renders the evaluation outcome: the JSON value, the CEL error, or an input error. */
function ResultPanel({
  result,
  error,
}: {
  result: CelEvalResult | null;
  error: string | null;
}) {
  if (error) {
    return (
      <div className="rounded-md border border-red-500/30 bg-red-500/5 px-3 py-2 text-xs text-red-500">
        {error}
      </div>
    );
  }
  if (!result) return null;
  if (!result.ok) {
    return (
      <div className="rounded-md border border-red-500/30 bg-red-500/5 px-3 py-2 font-mono text-xs text-red-500">
        {result.error ?? "evaluation failed"}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-emerald-600 dark:text-emerald-400">
        Result
      </span>
      <pre className="octo-code m-0 max-h-48 overflow-auto rounded-md border border-black/10 bg-zinc-50 p-2 text-xs dark:border-white/15 dark:bg-zinc-950">
        {JSON.stringify(result.result, null, 2)}
      </pre>
    </div>
  );
}
