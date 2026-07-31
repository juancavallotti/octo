"use client";

import { useState } from "react";
import { Settings2 } from "lucide-react";
import { addCase, removeCase, updateCase, uniqueCaseName } from "../../suite/edit";
import type { Suite } from "../../suite/types";
import type { SuiteIssue } from "../../suite/validate";
import CaseList from "./CaseList";
import CaseForm from "./CaseForm";
import SuiteSettingsPane from "./SuiteSettingsPane";
import EmptyState from "./EmptyState";

/** What the detail pane is showing: the file itself, or one of its cases. */
type Selection = number | "settings" | null;

/**
 * The form half of the Testing tab: the suite's settings and cases, and whichever one is
 * open.
 *
 * Every edit goes back out as a whole {@link Suite} for the caller to serialize, so this
 * component owns no copy of the file — there is one source of truth, the stored string,
 * and the form and the YAML view are always looking at the same thing.
 *
 * The detail pane is keyed on the selection so moving between cases (and between a case
 * and the settings) REMOUNTS it. That is what lets the fields inside seed their drafts
 * once and stop worrying: a JSON body being typed, a chosen-but-empty outcome, a name
 * that clashes and a half-named environment variable are all states the model cannot
 * hold, and the only moment they should be thrown away is when the user moves on.
 * Deleting the case being edited closes the pane rather than sliding the next one into
 * its place, so a half-typed field is never silently reattached to a different test.
 */
export default function SuiteFormView({
  suite,
  issues,
  onChange,
}: {
  suite: Suite;
  issues: SuiteIssue[];
  onChange: (next: Suite) => void;
}) {
  const [selected, setSelected] = useState<Selection>(suite.cases.length ? 0 : "settings");

  const add = () => {
    onChange(addCase(suite, uniqueCaseName(suite)));
    setSelected(suite.cases.length);
  };

  const remove = (index: number) => {
    onChange(removeCase(suite, index));
    if (index === selected) setSelected(null);
    else if (typeof selected === "number" && index < selected) setSelected(selected - 1);
  };

  const active =
    typeof selected === "number" && selected >= suite.cases.length ? null : selected;
  // A file-level problem is the settings pane's, so the row that opens it says so.
  const fileIssues = issues.filter((issue) => issue.caseIndex === undefined).length;

  return (
    <div className="flex min-h-0 flex-1">
      <div className="flex w-52 shrink-0 flex-col border-r border-black/10 dark:border-white/10">
        <button
          type="button"
          aria-current={active === "settings" ? "true" : undefined}
          onClick={() => setSelected("settings")}
          className={`flex shrink-0 items-center gap-1.5 border-b border-black/10 px-3 py-1.5 text-left text-xs transition-colors dark:border-white/10 ${
            active === "settings"
              ? "bg-black/10 text-zinc-900 dark:bg-white/15 dark:text-zinc-100"
              : "text-zinc-600 hover:bg-black/5 dark:text-zinc-300 dark:hover:bg-white/10"
          }`}
        >
          <Settings2 size={13} className="shrink-0" />
          <span className="min-w-0 flex-1 truncate">Suite settings</span>
          {fileIssues > 0 && (
            <span className="shrink-0 text-[11px] tabular-nums text-amber-600 dark:text-amber-400">
              {fileIssues}
            </span>
          )}
        </button>
        <CaseList
          cases={suite.cases}
          issues={issues}
          selected={typeof active === "number" ? active : null}
          onSelect={setSelected}
          onAdd={add}
          onRemove={remove}
        />
      </div>

      {active === "settings" ? (
        <SuiteSettingsPane key="settings" suite={suite} onChange={onChange} />
      ) : active === null ? (
        <EmptyState title={suite.cases.length ? "Pick a case" : "No cases yet"}>
          {suite.cases.length
            ? "Choose a case on the left, or add another."
            : "Add a case to say what this flow should do. A case with no expectation still asserts something: that the flow ran to the end without failing."}
        </EmptyState>
      ) : (
        <CaseForm
          key={active}
          suite={suite}
          index={active}
          onChange={(next) => onChange(updateCase(suite, active, next))}
        />
      )}
    </div>
  );
}
