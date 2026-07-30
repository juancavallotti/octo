"use client";

import { useState } from "react";
import { addCase, removeCase, updateCase, uniqueCaseName } from "../../suite/edit";
import type { Suite } from "../../suite/types";
import type { SuiteIssue } from "../../suite/validate";
import CaseList from "./CaseList";
import CaseForm from "./CaseForm";
import EmptyState from "./EmptyState";

/**
 * The form half of the Testing tab: the suite's cases, and the one being edited.
 *
 * Every edit goes back out as a whole {@link Suite} for the caller to serialize, so this
 * component owns no copy of the file — there is one source of truth, the stored string,
 * and the form and the YAML view are always looking at the same thing.
 *
 * The detail pane is keyed on the selected index so switching cases REMOUNTS it. That is
 * what lets the fields inside seed their drafts once and stop worrying: a JSON body being
 * typed, a chosen-but-empty outcome and a name that clashes are all states the model
 * cannot hold, and the only moment they should be thrown away is when the user moves to
 * another case. Deleting the case being edited clears the selection rather than sliding
 * the next one into its place, so a half-typed field is never silently reattached to a
 * different test.
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
  const [selected, setSelected] = useState<number | null>(suite.cases.length ? 0 : null);

  const add = () => {
    onChange(addCase(suite, uniqueCaseName(suite)));
    setSelected(suite.cases.length);
  };

  const remove = (index: number) => {
    onChange(removeCase(suite, index));
    if (index === selected) setSelected(null);
    else if (selected !== null && index < selected) setSelected(selected - 1);
  };

  const active = selected !== null && selected < suite.cases.length ? selected : null;

  return (
    <div className="flex min-h-0 flex-1">
      <CaseList
        cases={suite.cases}
        issues={issues}
        selected={active}
        onSelect={setSelected}
        onAdd={add}
        onRemove={remove}
      />
      {active === null ? (
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
