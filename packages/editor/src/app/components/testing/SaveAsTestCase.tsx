"use client";

import { useState } from "react";
import { Check, FlaskConical } from "lucide-react";
import { Popover } from "../../../components/ui";
import { useTestSuites } from "../../providers/TestSuiteProvider";
import type { FlowRunEntry } from "../../run/outcome";
import { suiteFileName } from "../../suite/naming";
import { parseSuite } from "../../suite/parse";
import type { PromotableRun } from "../../suite/promote";
import type { Outcome } from "../../suite/types";
import PromoteCaseForm from "./PromoteCaseForm";

/**
 * "Save as test case" on a run result — the second bridge between debugging and testing.
 *
 * A debugging session already contains a test: the input, the mocks that stood in for the
 * awkward blocks, and the message that came back. What it lacks is somewhere to live. This
 * writes it into the flow's suite, where CI and a terminal can both run it.
 *
 * Not every run can become one, and the button says which and why rather than quietly
 * appearing and disappearing — the FlowRunMenu pattern of a disabled control with a
 * `title`. The exclusions are not fussiness:
 *
 *   A run stopped at a **breakpoint** reports the message at that block, not the flow's
 *   result. Promoting it would assert that the flow returns what it was carrying halfway
 *   through, which is true of nothing.
 *
 *   A run that **timed out or never reached its block** has no outcome at all.
 *
 *   A suite the parser could not fully read cannot be rewritten without deleting the part
 *   it could not read — the same rule that disables the form view in the Testing tab.
 */
export default function SaveAsTestCase({ entry }: { entry: FlowRunEntry }) {
  const suites = useTestSuites();
  const [open, setOpen] = useState(false);
  const [saved, setSaved] = useState<string | null>(null);

  if (!suites) return null;

  if (saved) {
    return (
      <span className="flex items-center gap-1 text-[11px] text-emerald-600 dark:text-emerald-400">
        <Check size={12} aria-hidden />
        saved to {suiteFileName(entry.flowName)}
      </span>
    );
  }

  const why = blockedReason(entry, suites);

  return (
    <Popover
      open={open}
      onClose={() => setOpen(false)}
      panelClassName="right-0 w-80"
      trigger={
        <button
          type="button"
          disabled={!!why}
          title={why ?? "Save this run as a test case"}
          onClick={() => setOpen((o) => !o)}
          className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-zinc-500 hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-white/10"
        >
          <FlaskConical size={12} aria-hidden />
          Save as test case
        </button>
      }
    >
      <PromoteCaseForm
        flow={entry.flowName}
        run={promotable(entry)}
        onSaved={(name) => {
          setSaved(name);
          setOpen(false);
        }}
      />
    </Popover>
  );
}

/** Why this run cannot become a case, or null when it can. */
function blockedReason(
  entry: FlowRunEntry,
  suites: NonNullable<ReturnType<typeof useTestSuites>>,
): string | null {
  if (!suites.canPersist) return "Save this integration to give it tests.";
  if (entry.breakpointLabel) {
    return "A run stopped at a block reports the message there, not the flow's result. Run the whole flow to save it as a case.";
  }
  if (!outcomeOf(entry)) return "Only a run that finished has an outcome to assert.";

  const content = suites.suiteFor(entry.flowName);
  if (content && parseSuite(content).issues.some((i) => i.lossy)) {
    return `${suiteFileName(entry.flowName)} has something the editor cannot read, and rewriting it would lose that. Fix it in the Testing tab first.`;
  }
  return null;
}

/** The three run statuses that state what a flow did; anything else is not an outcome. */
function outcomeOf(entry: FlowRunEntry): Outcome | null {
  if (entry.status === "ok") return "message";
  if (entry.status === "dropped") return "dropped";
  if (entry.status === "error") return "error";
  return null;
}

function promotable(entry: FlowRunEntry): PromotableRun {
  return {
    outcome: outcomeOf(entry) ?? "message",
    message: entry.output,
    setup: entry.ran,
  };
}
