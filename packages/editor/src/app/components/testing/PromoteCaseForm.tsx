"use client";

import { useMemo, useState } from "react";
import { AlertTriangle } from "lucide-react";
import { useTestSuites } from "../../providers/TestSuiteProvider";
import { hasComments } from "../../suite/comments";
import { appendCase, nameTaken, uniqueCaseName } from "../../suite/edit";
import { parseSuite } from "../../suite/parse";
import { promoteRun, type PromotableRun } from "../../suite/promote";
import { serializeCase, serializeSuite } from "../../suite/serialize";
import { emptySuite } from "../../suite/types";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * Turning one run into a case: what to call it, how much of it to assert, and what the
 * file will get.
 *
 * The preview is not decoration. `expect.body` is an exact deep-equal, so a body carrying
 * a generated id or a timestamp produces a test that fails on its next honest run — and
 * the only reliable way to notice is to see the body before saving it. The warning names
 * the alternative (`that:`) rather than just cautioning.
 */
export default function PromoteCaseForm({
  flow,
  run,
  onSaved,
}: {
  flow: string;
  run: PromotableRun;
  /** The case name that was written, so the caller can say where it went. */
  onSaved: (name: string) => void;
}) {
  const suites = useTestSuites();
  const content = suites?.suiteFor(flow);
  const parsed = useMemo(
    () => (content ? parseSuite(content) : null),
    [content],
  );
  const suite = parsed?.suite ?? emptySuite(flow);

  const [name, setName] = useState(() =>
    uniqueCaseName(suite, "it does what it did"),
  );
  const [vars, setVars] = useState(false);
  const [spies, setSpies] = useState(false);
  const [errorContains, setErrorContains] = useState("");

  const promoted = promoteRun(run, {
    name,
    vars,
    spies,
    errorContains,
  });
  const taken = nameTaken(suite, name, -1);
  const needsError = run.outcome === "error" && errorContains.trim() === "";
  const blocked = name.trim() === "" || taken || needsError;

  const save = () => {
    if (blocked || !suites) return;
    suites.setSuite(flow, serializeSuite(appendCase(suite, promoted)));
    onSaved(promoted.name);
  };

  return (
    <div className="flex max-h-[26rem] flex-col gap-2 overflow-auto p-3">
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium text-zinc-500">Case name</span>
        <input
          type="text"
          value={name}
          autoFocus
          onChange={(e) => setName(e.target.value)}
          className={FIELD}
        />
        {taken && (
          <span className="text-xs text-amber-600 dark:text-amber-500">
            This suite already has a case by that name, and dolphin refuses a file with
            two.
          </span>
        )}
      </label>

      {run.outcome === "error" && (
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium text-zinc-500">Error contains</span>
          <input
            type="text"
            value={errorContains}
            placeholder="card declined"
            onChange={(e) => setErrorContains(e.target.value)}
            className={FIELD}
          />
          <span className="text-xs text-zinc-400 dark:text-zinc-500">
            What the console shows is the runner’s output, not the flow’s error, so say
            which phrase the failure must contain.
          </span>
        </label>
      )}

      {run.outcome === "message" && (
        <label className="flex items-center gap-2 text-xs text-zinc-500">
          <input type="checkbox" checked={vars} onChange={(e) => setVars(e.target.checked)} />
          Also assert the variables it set
        </label>
      )}
      {!!run.setup?.spies && Object.keys(run.setup.spies).length > 0 && (
        <label className="flex items-center gap-2 text-xs text-zinc-500">
          <input type="checkbox" checked={spies} onChange={(e) => setSpies(e.target.checked)} />
          Also assert how many messages crossed the spied blocks
        </label>
      )}

      {promoted.expect?.body !== undefined && (
        <p className="flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-500">
          <AlertTriangle size={12} className="mt-0.5 shrink-0" aria-hidden />
          The body is checked exactly, field for field. If it carries a generated id or a
          timestamp, assert it with <code>that:</code> instead once this is saved.
        </p>
      )}
      {content && hasComments(content) && (
        <p className="text-xs text-amber-600 dark:text-amber-500">
          Saving rewrites {flow}’s suite and drops its comments.
        </p>
      )}

      <pre className="overflow-x-auto rounded bg-black/[0.03] p-2 font-mono text-[11px] leading-relaxed text-zinc-700 dark:bg-white/[0.04] dark:text-zinc-300">
        {serializeCase(promoted)}
      </pre>

      <button
        type="button"
        disabled={blocked}
        onClick={save}
        className="self-end rounded-md bg-zinc-900 px-2.5 py-1 text-xs font-medium text-white disabled:opacity-40 dark:bg-white dark:text-zinc-900"
      >
        {content ? "Add to the suite" : "Create the suite"}
      </button>
    </div>
  );
}
