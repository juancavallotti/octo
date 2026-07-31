"use client";

import { useState } from "react";
import type { MessageExpect, RecordExpect } from "../../suite/types";
import MessageExpectFields from "./MessageExpectFields";
import Segmented from "./Segmented";

/** The three ways out of a block. `input` is not one of them — see below. */
type Way = "output" | "dropped" | "error";

const WAYS: { key: Way; label: string }[] = [
  { key: "output", label: "Output" },
  { key: "dropped", label: "Dropped" },
  { key: "error", label: "Error" },
];

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

function wayOf(r: RecordExpect | undefined): Way {
  if (r?.error) return "error";
  if (r?.dropped) return "dropped";
  return "output";
}

/**
 * One crossing of a watched block: what went in, and what came out.
 *
 * `input` sits outside the three-way switch on purpose, and that is the format being
 * precise rather than sloppy. The spy clones the message BEFORE the block runs, so even a
 * block that failed shows what it was carrying when it did — which is the whole reason to
 * spy on a block you have mocked into failing. What came out is the exclusive part: a
 * block either returned a message, dropped it, or failed.
 */
export default function RecordField({
  value,
  onChange,
}: {
  value: RecordExpect;
  onChange: (next: RecordExpect) => void;
}) {
  const [way, setWay] = useState<Way>(() => wayOf(value));

  const pick = (next: Way) => {
    setWay(next);
    const input = value.input;
    if (next === "dropped") return onChange(clean({ input, dropped: true }));
    if (next === "error") return onChange(clean({ input, error: value.error ?? "" }));
    onChange(clean({ input, output: value.output }));
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-col gap-1.5">
        <span className="text-xs font-medium text-zinc-500">
          Going in
          <span className="ml-1 font-normal text-zinc-400 dark:text-zinc-500">
            captured before the block ran, even if it failed
          </span>
        </span>
        <MessageExpectFields
          value={value.input}
          bodyPlaceholder='{ "orderId": 42 }'
          onChange={(input) => onChange(clean({ ...value, input }))}
        />
      </div>

      <div className="flex items-center gap-2">
        <span className="text-xs font-medium text-zinc-500">Coming out</span>
        <Segmented options={WAYS} value={way} onChange={pick} ariaLabel="Crossing outcome" />
      </div>

      {way === "output" && (
        <MessageExpectFields
          value={value.output}
          onChange={(output) => onChange(clean({ ...value, output }))}
        />
      )}

      {way === "dropped" && (
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          The block filtered the message out.
        </p>
      )}

      {way === "error" && (
        <input
          type="text"
          value={value.error ?? ""}
          placeholder="card declined"
          onChange={(e) => onChange(clean({ ...value, error: e.target.value }))}
          className={FIELD}
        />
      )}
    </div>
  );
}

/** Keep only what the record states; the three ways out are mutually exclusive. */
function clean(r: {
  input?: MessageExpect;
  output?: MessageExpect;
  dropped?: boolean;
  error?: string;
}): RecordExpect {
  const out: RecordExpect = {};
  if (r.input) out.input = r.input;
  if (r.output) out.output = r.output;
  if (r.dropped) out.dropped = true;
  if (r.error) out.error = r.error;
  return out;
}
