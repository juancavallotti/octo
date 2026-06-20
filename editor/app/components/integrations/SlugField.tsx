"use client";

import { useEffect, useState } from "react";
import { Check, Link2, Loader2, X } from "lucide-react";
import { getDeployOptions } from "@/app/model/orchestrator";

/**
 * Text input for a deployment's address slug with live, debounced availability
 * checking against the orchestrator. The slug names the internal Service
 * (octo-int-{slug}); when the deploy is exposed externally it is also the public
 * host label, so the check includes the subdomain (driven by `expose`). Validity is
 * reported to the parent via onValidChange so it can gate the Deploy button.
 */

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

type Status = "idle" | "checking" | "ok" | "taken" | "invalid" | "error";

// The outcome of one validation, tagged with the input it was for so a stale result
// is treated as "still checking" once the input changes.
type Result = {
  slug: string;
  expose: boolean;
  valid: boolean;
  available: boolean;
  failed: boolean;
};

export default function SlugField({
  integrationId,
  value,
  onChange,
  expose,
  busy,
  onValidChange,
}: {
  integrationId: string;
  value: string;
  onChange: (v: string) => void;
  expose: boolean;
  busy: boolean;
  onValidChange: (ok: boolean) => void;
}) {
  const [result, setResult] = useState<Result | null>(null);
  const trimmed = value.trim();

  // Debounced validation: schedule a check, storing the tagged result. State is set
  // only in the async callback (never synchronously in the effect body), and the
  // result is matched against the current input during render.
  useEffect(() => {
    if (!trimmed) return;
    const timer = setTimeout(() => {
      getDeployOptions(integrationId, {
        slug: trimmed,
        expose: expose ? "external" : undefined,
      }).then(
        (o) =>
          setResult({
            slug: trimmed,
            expose,
            valid: o.slugValid,
            available: o.slugAvailable,
            failed: false,
          }),
        () =>
          setResult({ slug: trimmed, expose, valid: false, available: false, failed: true }),
      );
    }, 350);
    return () => clearTimeout(timer);
  }, [integrationId, trimmed, expose]);

  const fresh = result !== null && result.slug === trimmed && result.expose === expose;
  let status: Status = "checking";
  if (!trimmed) status = "idle";
  else if (fresh && result) {
    if (result.failed) status = "error";
    else if (!result.valid) status = "invalid";
    else status = result.available ? "ok" : "taken";
  }

  // Report validity to the parent (so it can gate Deploy) whenever it changes.
  const ok = status === "ok";
  useEffect(() => {
    onValidChange(ok);
  }, [ok, onValidChange]);

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <Link2 size={14} className="shrink-0 text-zinc-400" />
        <input
          type="text"
          value={value}
          disabled={busy}
          onChange={(e) => onChange(e.target.value)}
          placeholder="address-slug"
          spellCheck={false}
          autoCapitalize="off"
          className={`${INPUT} flex-1 font-mono`}
        />
      </div>
      <div className="flex min-h-[1rem] items-center gap-1 pl-6 text-[11px]">
        <SlugStatus status={status} />
      </div>
    </div>
  );
}

function SlugStatus({ status }: { status: Status }) {
  switch (status) {
    case "checking":
      return (
        <span className="inline-flex items-center gap-1 text-zinc-400">
          <Loader2 size={12} className="animate-spin" /> checking…
        </span>
      );
    case "ok":
      return (
        <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
          <Check size={12} /> available
        </span>
      );
    case "taken":
      return (
        <span className="inline-flex items-center gap-1 text-red-500">
          <X size={12} /> already in use
        </span>
      );
    case "invalid":
      return (
        <span className="inline-flex items-center gap-1 text-red-500">
          <X size={12} /> not a valid slug
        </span>
      );
    case "error":
      return (
        <span className="text-amber-600 dark:text-amber-400">
          couldn’t check availability
        </span>
      );
    default:
      return null;
  }
}
