"use client";

import { useState } from "react";
import { Eye, TestTube } from "lucide-react";
import { Popover } from "../../components/ui";
import { useRun } from "../run/RunContext";
import { useBlockDebug } from "../run/useBlockDebug";
import MockForm from "./MockForm";
import SpyBadge from "./SpyBadge";

/**
 * The two debug affordances that sit beside "run to here" on a node: stand this block in
 * for something else, and watch what crosses it.
 *
 * Both hide themselves when the block cannot carry them — an unaddressable branch, a flow
 * whose name breaks the address grammar — rather than offering something that cannot work,
 * exactly as the run button already does.
 *
 * An active mock or spy keeps its button *visible* rather than revealing it on hover: it
 * is a standing change to what a run does, and a mock the user cannot see is the whole
 * problem mocks create.
 */
export default function BlockDebugButtons({ blockId }: { blockId: string }) {
  const run = useRun();
  const debug = useBlockDebug(blockId);
  const [editing, setEditing] = useState(false);

  if (!run?.available || !debug?.supported) return null;

  const mocked = debug.mock?.enabled === true;
  const hasMock = debug.mock !== undefined;

  return (
    <>
      <Popover
        open={editing}
        onClose={() => setEditing(false)}
        panelClassName="right-0 w-80"
        trigger={
          <button
            type="button"
            aria-label={hasMock ? "Edit mock" : "Mock this block"}
            title={
              mocked
                ? "This block is mocked: the real one does not run"
                : hasMock
                  ? "This block has a mock, currently off"
                  : "Stand in for this block, so the real one never runs"
            }
            onClick={(e) => {
              e.stopPropagation();
              setEditing((open) => !open);
            }}
            className={`rounded-full border border-black/10 bg-white p-0.5 shadow-sm transition-opacity hover:text-amber-600 dark:border-white/15 dark:bg-zinc-900 dark:hover:text-amber-400 ${
              hasMock
                ? `opacity-100 ${mocked ? "text-amber-600 dark:text-amber-400" : "text-zinc-400"}`
                : "text-zinc-400 opacity-0 group-hover:opacity-100"
            }`}
          >
            <TestTube size={14} />
          </button>
        }
      >
        {editing && (
          <MockForm
            initial={debug.mock}
            onSubmit={(mock) => {
              debug.setMock(mock);
              setEditing(false);
            }}
            onRemove={
              hasMock
                ? () => {
                    debug.removeMock();
                    setEditing(false);
                  }
                : undefined
            }
            onCancel={() => setEditing(false)}
          />
        )}
      </Popover>

      {/* The count of what the spy has collected rides ON the eye, as a notification dot
          does — it is what that button produced, so it belongs to it. Floating loose in the
          row it read as a fourth control rather than as a result. */}
      <div className="relative">
        <button
          type="button"
          aria-label={debug.spied ? "Stop watching this block" : "Watch this block"}
          title={
            debug.spied
              ? "Watching: every message crossing this block is recorded"
              : "Record every message that crosses this block"
          }
          onClick={(e) => {
            e.stopPropagation();
            debug.toggleSpy();
          }}
          className={`block rounded-full border border-black/10 bg-white p-0.5 shadow-sm transition-opacity hover:text-violet-600 dark:border-white/15 dark:bg-zinc-900 dark:hover:text-violet-400 ${
            debug.spied || debug.records.length > 0
              ? "text-violet-600 opacity-100 dark:text-violet-400"
              : "text-zinc-400 opacity-0 group-hover:opacity-100"
          }`}
        >
          <Eye size={14} />
        </button>
        <div className="absolute -right-1.5 -top-1.5">
          <SpyBadge blockId={blockId} />
        </div>
      </div>
    </>
  );
}
