"use client";

import { useState } from "react";
import { Popover } from "../../components/ui";
import { useBlockDebug } from "../run/useBlockDebug";
import type { SpyRecord } from "../run/transport";

/**
 * What a spied block has seen — a count on the watch button, and the messages behind it.
 *
 * The records reach the Results tab too, in a sense, but at the bottom of the screen and
 * detached from the block they came from — which is exactly the connection the user turned
 * the spy on to make. So they surface here instead, as a notification dot on the eye that
 * collected them.
 *
 * They accumulate across runs (see FlowRunContext), which is what makes the count worth
 * showing: the interesting thing about a spy is usually how run 2 differs from run 1.
 *
 * It renders inline; the caller places it over the control it belongs to (see
 * BlockDebugButtons) — it is a badge ON a control, not a control of its own.
 */
export default function SpyBadge({ blockId }: { blockId: string }) {
  const debug = useBlockDebug(blockId);
  const [open, setOpen] = useState(false);

  if (!debug || debug.records.length === 0) return null;

  return (
    <Popover
      open={open}
      onClose={() => setOpen(false)}
      panelClassName="right-0 w-96"
      trigger={
        <button
          type="button"
          aria-label={`${debug.records.length} recorded messages`}
          title="What crossed this block"
          onClick={(e) => {
            e.stopPropagation();
            setOpen((was) => !was);
          }}
          className="flex h-4 min-w-4 items-center justify-center rounded-full bg-violet-600 px-1 text-[9px] font-semibold leading-none text-white shadow-sm ring-2 ring-white hover:bg-violet-500 dark:ring-zinc-900"
        >
          {debug.records.length}
        </button>
      }
    >
      {open && (
        <div className="flex max-h-[26rem] flex-col">
          <div className="flex items-center justify-between border-b border-black/10 px-3 py-2 dark:border-white/10">
            <span className="truncate text-xs font-medium text-zinc-500" title={debug.address ?? ""}>
              {debug.address}
            </span>
            <button
              type="button"
              onClick={() => {
                debug.clearRecords();
                setOpen(false);
              }}
              className="shrink-0 rounded-md px-1.5 py-0.5 text-xs text-zinc-500 hover:bg-black/[0.05] dark:hover:bg-white/10"
            >
              Clear
            </button>
          </div>
          <ol className="flex flex-col divide-y divide-black/[0.06] overflow-auto dark:divide-white/[0.06]">
            {debug.records.map((record) => (
              <li key={record.seq} className="p-3">
                <Record record={record} />
              </li>
            ))}
          </ol>
        </div>
      )}
    </Popover>
  );
}

/**
 * One crossing: what went in, and what came out.
 *
 * The three outcomes are meaningfully different and must not be flattened into "the
 * output". A block that DROPPED the message and a block that FAILED both produced no
 * message, and telling them apart is half of why a spy is on in the first place.
 */
function Record({ record }: { record: SpyRecord }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <span className="rounded-full bg-black/[0.06] px-1.5 py-0.5 text-[10px] leading-none text-zinc-500 dark:bg-white/10">
          #{record.seq}
        </span>
        {record.error !== undefined && (
          <span className="rounded-full bg-rose-500/10 px-1.5 py-0.5 text-[10px] leading-none text-rose-600 dark:text-rose-400">
            failed
          </span>
        )}
        {record.dropped && (
          <span className="rounded-full bg-amber-500/10 px-1.5 py-0.5 text-[10px] leading-none text-amber-600 dark:text-amber-500">
            dropped
          </span>
        )}
      </div>

      <Message label="in" value={record.input} />
      {record.error !== undefined ? (
        <p className="text-xs text-rose-500">{record.error}</p>
      ) : record.dropped ? (
        <p className="text-xs text-zinc-500">
          The block filtered the message out; nothing came back.
        </p>
      ) : (
        <Message label="out" value={record.output} />
      )}
    </div>
  );
}

function Message({ label, value }: { label: string; value: unknown }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] font-medium uppercase text-zinc-400">{label}</span>
      <pre className="max-h-32 overflow-auto rounded-md bg-black/[0.03] p-1.5 text-[11px] leading-snug dark:bg-white/[0.04]">
        {JSON.stringify(value, null, 2)}
      </pre>
    </div>
  );
}
