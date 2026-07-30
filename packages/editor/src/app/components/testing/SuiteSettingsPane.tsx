"use client";

import { isValidDuration, type Suite } from "../../suite/types";
import { DEFAULT_CASE_TIMEOUT } from "../../suite/merge";
import KeyValueField from "./KeyValueField";
import SuiteInputsField from "./SuiteInputsField";
import MocksField from "./MocksField";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * What the whole file says, as opposed to what one case says: the environment every case
 * runs with, the shared inputs they name, and the timeout that bounds them.
 *
 * `env:` is the one that decides whether the tab can author a working suite at all. A
 * config's environment is resolved when it is LOADED, before any block runs, so a flow
 * whose connector reads `${ANTHROPIC_API_KEY}` cannot even be BUILT without one — and
 * mocking the block that uses it does not help, because the mock replaces the block after
 * the config has already failed to load. Without this pane the suites you most want are
 * the ones you cannot write.
 *
 * The values here are fake by construction, and that is what makes them safe to commit:
 * a suite passes a key to a block that has been stood in for, so it only ever has to be
 * shaped like a credential, never be one.
 *
 * `flow:` is shown and not edited. The file is stored under its flow's name, so changing
 * it here would orphan the suite from the flow it tests; renaming happens on the canvas,
 * and the tab follows.
 */
export default function SuiteSettingsPane({
  suite,
  onChange,
}: {
  suite: Suite;
  onChange: (next: Suite) => void;
}) {
  const badTimeout = !!suite.timeout && !isValidDuration(suite.timeout);

  const set = <K extends keyof Suite>(key: K, next: Suite[K] | undefined) => {
    const out = { ...suite };
    if (next === undefined) delete out[key];
    else out[key] = next;
    onChange(out);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-3">
      <div className="flex flex-col gap-1">
        <span className="text-xs font-medium text-zinc-500">Flow under test</span>
        <p className="rounded-md border border-dashed border-black/10 px-2 py-1 font-mono text-sm text-zinc-600 dark:border-white/15 dark:text-zinc-300">
          {suite.flow}
        </p>
        <span className="text-[11px] text-zinc-400 dark:text-zinc-500">
          Every case in this file calls it. Rename the flow on the canvas and the suite
          follows.
        </span>
      </div>

      <label className="flex flex-col gap-1">
        <span className="flex items-baseline justify-between text-xs font-medium text-zinc-500">
          Timeout
          <span className="font-normal text-zinc-400 dark:text-zinc-500">
            bounds every case; a case may shorten or lengthen it
          </span>
        </span>
        <input
          type="text"
          value={suite.timeout ?? ""}
          placeholder={DEFAULT_CASE_TIMEOUT}
          onChange={(e) => set("timeout", e.target.value || undefined)}
          className={`${FIELD} ${badTimeout ? "border-rose-400/70" : ""}`}
        />
        {badTimeout && (
          <span className="text-[11px] text-rose-500">
            Not a Go duration — try 30s, 1m30s or 500ms.
          </span>
        )}
      </label>

      <KeyValueField
        label="Environment"
        hint={
          <>
            A config&apos;s environment resolves when it <strong>loads</strong>, before any
            block runs — so a flow whose connector reads <code>${"{SOME_KEY}"}</code> needs
            one here even if you mock the block that uses it. These are fake by
            construction, which is what makes them safe to commit.
          </>
        }
        value={suite.env}
        onChange={(env) => set("env", env)}
        keyPlaceholder="SOME_KEY"
        valuePlaceholder="not-a-real-key"
        addLabel="Add variable"
      />

      <SuiteInputsField
        value={suite.inputs}
        onChange={(inputs) => set("inputs", inputs)}
      />

      <MocksField
        flow={suite.flow}
        label="Mocks"
        hint="Stand-ins for blocks in every case, unless a case overrides the address."
        value={suite.mocks}
        onChange={(mocks) => set("mocks", mocks)}
      />
    </div>
  );
}
