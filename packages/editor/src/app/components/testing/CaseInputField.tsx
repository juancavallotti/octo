"use client";

import { isSharedInput, type CaseInput, type SuiteInput } from "../../suite/types";
import JsonValueField from "./JsonValueField";
import Segmented from "./Segmented";

/** The three things a case's input can be, and what each one writes. */
type Kind = "none" | "shared" | "inline";

const KINDS: { key: Kind; label: string }[] = [
  { key: "none", label: "None" },
  { key: "shared", label: "Shared" },
  { key: "inline", label: "Inline" },
];

function kindOf(input: CaseInput | undefined): Kind {
  if (input === undefined) return "none";
  return isSharedInput(input) ? "shared" : "inline";
}

/**
 * What the flow is called with.
 *
 * Three-way rather than two fields, because the format is: `input:` is either the name of
 * something declared in `inputs:` or an inline `{data, vars}`, never both. A body six
 * cases share wants a name; a body used once does not.
 *
 * `vars` is offered as prominently as `data` on purpose. An invoked flow starts no
 * sources, so nothing populates the message variables — a flow reading a header the HTTP
 * source would normally have copied across sees nothing unless the case supplies it. It
 * is the most common reason a flow behaves differently under test than in production.
 */
export default function CaseInputField({
  value,
  declared,
  onChange,
}: {
  value: CaseInput | undefined;
  /** The names in the file's `inputs:`, which is all a shared reference may name. */
  declared: string[];
  onChange: (next: CaseInput | undefined) => void;
}) {
  const kind = kindOf(value);
  const inline: SuiteInput = kind === "inline" ? (value as SuiteInput) : {};

  // A reference to an input that is not declared is a load error, reported by the rules.
  // Keep it in the list anyway: silently swapping it for a valid one would edit the
  // user's file behind their back rather than show them the problem.
  const options = Array.from(
    new Set([...declared, ...(isSharedInput(value) && value ? [value] : [])]),
  );

  const pick = (next: Kind) => {
    if (next === "none") return onChange(undefined);
    if (next === "shared") return onChange(options[0] ?? "");
    onChange({});
  };

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <h3 className="text-xs font-semibold text-zinc-600 dark:text-zinc-300">Input</h3>
        <Segmented
          options={KINDS}
          value={kind}
          onChange={pick}
          disabled={declared.length === 0 ? ["shared"] : []}
          disabledReason="This file declares no shared inputs yet — add an `inputs:` section in the YAML view."
          ariaLabel="Input source"
        />
      </div>

      {kind === "none" && (
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          The flow is called with an empty message.
        </p>
      )}

      {kind === "shared" && (
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium text-zinc-500">Declared in `inputs:`</span>
          <select
            value={isSharedInput(value) ? value : ""}
            onChange={(e) => onChange(e.target.value)}
            className="w-full rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
          >
            {options.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </label>
      )}

      {kind === "inline" && (
        <>
          <JsonValueField
            label="Body"
            hint="the message the flow is called with"
            value={inline.data}
            placeholder='{ "orderId": 42 }'
            onChange={(data) => onChange(stripped({ ...inline, data }))}
          />
          <JsonValueField
            label="Variables"
            hint="what a source would have set"
            value={inline.vars}
            objectOnly
            placeholder='{ "x-api-key": "dev" }'
            onChange={(vars) =>
              onChange(stripped({ ...inline, vars: vars as Record<string, unknown> }))
            }
          />
        </>
      )}
    </section>
  );
}

/** Drop keys the fields cleared, so an emptied inline input serializes as `input: {}`. */
function stripped(input: SuiteInput): SuiteInput {
  const out: SuiteInput = {};
  if (input.data !== undefined) out.data = input.data;
  if (input.vars !== undefined) out.vars = input.vars;
  return out;
}
