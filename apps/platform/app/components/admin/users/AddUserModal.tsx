"use client";

import { useEffect, useState } from "react";
import { UserPlus, X } from "lucide-react";
import { createUser } from "@/app/model/users";
import { Field, INPUT } from "../fields";

/**
 * Adding somebody who has never signed in.
 *
 * An address and, optionally, a name. Nothing else is asked for because nothing
 * else is known: the person has never been here, and what identifies them to the
 * identity provider is written by their own first sign-in.
 *
 * It closes on a successful add and tells its host to reload. Staying open for
 * the next person would be the right shape for bulk work, and it is the wrong
 * one for this: the list behind the dialog is the confirmation that the add
 * landed, and an administrator adding a team wants to see each one arrive.
 */
export default function AddUserModal({
  onAdded,
  onClose,
}: {
  onAdded: () => void;
  onClose: () => void;
}) {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Close on Escape, mirroring the platform's other overlays.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !busy) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onClose]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await createUser({ email: email.trim(), name: name.trim() });
      onAdded();
      onClose();
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Add a person"
      onMouseDown={() => !busy && onClose()}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm"
    >
      <form
        onSubmit={submit}
        onMouseDown={(e) => e.stopPropagation()}
        className="flex w-full max-w-md flex-col overflow-hidden rounded-xl border border-black/10 bg-white shadow-xl dark:border-white/10 dark:bg-zinc-900"
      >
        <header className="flex items-center gap-2 border-b border-black/10 px-4 py-3 dark:border-white/10">
          <UserPlus size={16} className="text-sky-500" />
          <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">Add a person</h3>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            disabled={busy}
            className="rounded p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/[0.08] dark:hover:text-zinc-200"
          >
            <X size={16} />
          </button>
        </header>

        <div className="flex flex-col gap-3 px-4 py-4">
          <Field
            label="Email"
            hint="The address they sign in with. Their first sign-in claims this account."
          >
            <input
              className={INPUT}
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              autoFocus
              placeholder="person@example.com"
            />
          </Field>
          <Field label="Name" hint="Optional — their next sign-in refreshes it anyway.">
            <input
              className={INPUT}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Ada Lovelace"
            />
          </Field>
          {error && <p className="text-xs text-red-600 dark:text-red-400">{error}</p>}
        </div>

        <footer className="flex justify-end gap-2 border-t border-black/10 px-4 py-3 dark:border-white/10">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="rounded-md px-3 py-1 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] disabled:opacity-50 dark:text-zinc-300 dark:hover:bg-white/[0.06]"
          >
            Cancel
          </button>
          {/* A real submit rather than the shared PrimaryButton, which is a
              type="button" — inside a form, Enter should add the person. */}
          <button
            type="submit"
            disabled={busy || !email.trim()}
            className="rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
          >
            {busy ? "Adding…" : "Add"}
          </button>
        </footer>
      </form>
    </div>
  );
}
