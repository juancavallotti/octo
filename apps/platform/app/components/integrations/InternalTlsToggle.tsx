"use client";

import { Lock } from "lucide-react";

/**
 * The deploy dialog's switch for serving HTTPS inside the cluster instead of
 * plain HTTP. Disabled, and said so, when the installation has no internal certificate
 * authority to issue the certificate from.
 */
export default function InternalTlsToggle({
  available,
  checked,
  busy,
  onChange,
}: {
  available: boolean;
  checked: boolean;
  busy: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <label
      className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300"
      title={
        available
          ? undefined
          : "This installation has no internal certificate authority configured."
      }
    >
      <input
        type="checkbox"
        checked={available && checked}
        disabled={busy || !available}
        onChange={(e) => onChange(e.target.checked)}
        className="accent-sky-500"
      />
      <Lock size={14} />
      Serve HTTPS inside the cluster
      {!available && (
        <span className="text-xs text-zinc-400">(not configured)</span>
      )}
    </label>
  );
}
