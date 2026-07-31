"use client";

import { useMemo, useState } from "react";
import { Trash2 } from "lucide-react";
import { useRun } from "../../run/RunContext";
import { useSuiteRun } from "../../run/SuiteRunContext";
import { parseSuite } from "../../suite/parse";
import { serializeSuite } from "../../suite/serialize";
import { hasComments } from "../../suite/comments";
import { suiteFileName } from "../../suite/naming";
import { totalsOf } from "../../run/testTransport";
import type { Suite } from "../../suite/types";
import TestingToolbar, { type SuiteViewMode } from "./TestingToolbar";
import SuiteIssues from "./SuiteIssues";
import SuiteFormView from "./SuiteFormView";
import SuiteYamlEditor from "./SuiteYamlEditor";

/**
 * One open suite: its toolbar, its problems, and either view of it.
 *
 * The stored string is the source of truth in both views. The YAML view saves it byte for
 * byte; the form parses it, edits the model and re-serializes. That asymmetry is the
 * whole reason the two are separate rather than one editor with two skins, and it has two
 * consequences the tab has to be honest about:
 *
 *   **Comments do not survive the form.** The model has nowhere to put them, so the first
 *   edit through the form drops every one. Said in a banner while there is still something
 *   to lose, with the YAML view one click away.
 *
 *   **A file we could not fully read must not be written back from the form.** Anything
 *   the parser did not understand is absent from the model, so re-serializing would
 *   DELETE it — silently, and in a file the user is about to commit. Those issues are
 *   flagged `lossy` at the parse, and while any stand the form is unavailable and the
 *   YAML view is all there is.
 */
export default function SuiteEditor({
  flow,
  content,
  onChange,
  onRemove,
}: {
  flow: string;
  content: string;
  onChange: (next: string) => void;
  onRemove: () => void;
}) {
  const run = useRun();
  const suiteRun = useSuiteRun();
  const [wanted, setWanted] = useState<SuiteViewMode>("form");

  const { suite, issues } = useMemo(() => parseSuite(content), [content]);
  const lossy = issues.some((issue) => issue.lossy);
  const mode = lossy ? "yaml" : wanted;
  const commented = useMemo(() => hasComments(content), [content]);

  // The store is keyed by flow name, so a suite that lost its `flow:` — by an edit in the
  // YAML view, or by starting from an empty file — is unambiguously wrong rather than
  // ambiguous. Put it back rather than write a file dolphin cannot load.
  const apply = (next: Suite) =>
    onChange(serializeSuite({ ...next, flow: next.flow || flow }));

  // This toolbar speaks for THIS suite, so it reports this suite's slice of the last run —
  // a run started from the header carries several, and the whole-run tally would credit
  // this file with cases that are not in it. Nothing here means the last run did not
  // include it, which is why a "no cases ran" tally is not the same as no tally at all.
  const mine = suiteRun?.outcome?.suites.find((s) => s.name === flow) ?? null;

  return (
    <>
      <TestingToolbar
        fileName={suiteFileName(flow)}
        cases={suite.cases.length}
        totals={mine ? totalsOf(mine.cases) : null}
        running={!!suiteRun?.running && suiteRun.flows.includes(flow)}
        busy={suiteRun?.running ?? false}
        testAvailable={run?.testAvailable ?? false}
        mode={mode}
        onMode={setWanted}
        formBlockedReason={
          lossy
            ? "This file says something the form cannot represent, so editing it here would delete that. Fix the problems below first."
            : undefined
        }
        // A suite dolphin would refuse takes every case in it down, so running is not
        // something to find out about the hard way.
        blockedReason={
          issues.length > 0
            ? "Fix the problems above: dolphin will not load this suite."
            : undefined
        }
        onRun={() => void suiteRun?.run([{ name: flow, content }])}
      >
        <button
          type="button"
          aria-label={`Delete tests for ${flow}`}
          title={`Delete tests for ${flow}`}
          onClick={onRemove}
          className="shrink-0 rounded p-1 text-zinc-400 hover:bg-red-500/10 hover:text-red-600 dark:hover:text-red-400"
        >
          <Trash2 size={13} />
        </button>
      </TestingToolbar>

      {mode === "form" && commented && (
        <p className="shrink-0 border-b border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-[11px] text-amber-800 dark:text-amber-200/90">
          Editing in the form reformats the file and drops its comments.{" "}
          <button
            type="button"
            onClick={() => setWanted("yaml")}
            className="underline underline-offset-2 hover:no-underline"
          >
            Switch to YAML
          </button>{" "}
          to keep them.
        </p>
      )}

      <SuiteIssues issues={issues} />

      {mode === "form" ? (
        <SuiteFormView suite={suite} issues={issues} onChange={apply} />
      ) : (
        <SuiteYamlEditor value={content} onChange={onChange} />
      )}
    </>
  );
}
