"use client";

import type { ReactNode } from "react";

/**
 * The form primitives both admin settings pages share: the input styling, a
 * labelled field, the API-key row, and the banner shown when this orchestrator
 * cannot encrypt a key at all.
 *
 * Extracted because the email and LLM managers are the same form with different
 * fields, and the API-key row in particular carries behaviour worth writing once —
 * it never displays a stored key, only whether one exists and its last four
 * characters.
 */

export const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30 disabled:opacity-50";

/** A labelled form row. */
export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs font-medium text-zinc-600 dark:text-zinc-300">
        {label}
      </span>
      {children}
      {hint && <span className="text-xs text-zinc-500">{hint}</span>}
    </label>
  );
}

/**
 * Shown when the orchestrator has no encryption key. Storing a secret is refused in
 * that state rather than done in the clear, so the operator is told up front and
 * pointed at the setting, instead of finding out when a save fails.
 */
export function EncryptionWarning() {
  return (
    <p className="mt-3 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
      Storing an API key is disabled: this orchestrator has no encryption key. Set{" "}
      <code className="font-mono">kv.encryptionKey</code> in the Helm values to
      enable it. The other settings on this page still save.
    </p>
  );
}

/**
 * The API-key input. A stored key is never rendered — the field starts empty and
 * stays empty on reload, and what a stored key looks like is a note beneath it.
 * Leaving it blank on save keeps whatever is stored.
 */
export function ApiKeyField({
  value,
  onChange,
  configured,
  last4,
  disabled,
  placeholder,
  onRemove,
}: {
  value: string;
  onChange: (next: string) => void;
  configured: boolean;
  last4: string;
  disabled: boolean;
  placeholder: string;
  onRemove: () => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <Field label="API key">
        <input
          type="password"
          value={value}
          disabled={disabled}
          placeholder={placeholder}
          autoComplete="new-password"
          onChange={(e) => onChange(e.target.value)}
          className={`${INPUT} w-full font-mono`}
        />
      </Field>
      <div className="flex items-center gap-2">
        <span className="text-xs text-zinc-500">
          {configured ? (
            <>
              A key ending{" "}
              <code className="font-mono">••••{last4}</code> is stored. Leave this
              blank to keep it.
            </>
          ) : (
            "No API key stored."
          )}
        </span>
        {configured && (
          <button
            type="button"
            onClick={onRemove}
            className="text-xs font-medium text-red-500 hover:underline"
          >
            Remove
          </button>
        )}
      </div>
    </div>
  );
}

/** The primary action button, matching the rest of the platform's forms. */
export function PrimaryButton({
  onClick,
  disabled,
  children = "Save",
}: {
  onClick: () => void;
  disabled: boolean;
  children?: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="self-start rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
    >
      {children}
    </button>
  );
}
