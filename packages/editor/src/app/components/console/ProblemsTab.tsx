"use client";

import { CircleCheck, TriangleAlert } from "lucide-react";
import { EditorActionType, useEditorState } from "../../state/editorState";
import type { Issue } from "../../model/validate";

/**
 * Everything standing between the document and a run: the validation issues, plus the
 * errors the last manual run reported. Clicking an issue that knows which block it
 * belongs to selects that block, so a problem is one click from its cause.
 *
 * Validation is recomputed on every document edit (RunContext memoizes it), so this is
 * always current without anything polling.
 */
export default function ProblemsTab({
  issues,
  runErrors,
}: {
  issues: Issue[];
  /** Errors from the last manual flow run — a failure the document alone can't predict. */
  runErrors: string[];
}) {
  const { dispatch } = useEditorState();

  if (issues.length === 0 && runErrors.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center gap-2 text-xs text-zinc-400 dark:text-zinc-600">
        <CircleCheck size={14} />
        No problems detected.
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-auto py-1 text-xs">
      {issues.map((issue, i) => {
        const selectable = !!issue.blockId;
        return (
          <button
            key={`${issue.message}-${i}`}
            type="button"
            disabled={!selectable}
            onClick={() => {
              if (issue.blockId) {
                dispatch({
                  type: EditorActionType.SELECT_BLOCK,
                  data: { blockId: issue.blockId },
                });
              }
            }}
            className={`flex w-full items-start gap-2 px-3 py-1 text-left ${
              selectable
                ? "cursor-pointer hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                : "cursor-default"
            }`}
          >
            <TriangleAlert
              size={13}
              className="mt-0.5 shrink-0 text-rose-500 dark:text-rose-400"
              aria-hidden
            />
            <span className="text-zinc-700 dark:text-zinc-300">{issue.message}</span>
          </button>
        );
      })}

      {runErrors.length > 0 && (
        <div className="mt-1 border-t border-black/5 pt-1 dark:border-white/5">
          {runErrors.map((line, i) => (
            <div
              key={`${line}-${i}`}
              className="flex items-start gap-2 px-3 py-1 font-mono text-[11px] text-rose-600 dark:text-rose-400"
            >
              <span className="whitespace-pre-wrap break-words">{line}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
