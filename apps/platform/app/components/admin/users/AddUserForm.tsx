"use client";

import { useState } from "react";
import { createUser } from "@/app/model/users";
import { Field, INPUT } from "../fields";

/**
 * Adding somebody who has never signed in.
 *
 * The subject is the awkward field and cannot be avoided: this platform admits
 * only provisioned users, an account has to exist before its owner can sign in,
 * and the only thing that identifies them to us is the `sub` their identity
 * provider will present. We cannot discover it, and deriving it from an email
 * address would be a way for one person to be admitted as another — so it is
 * asked for, with a hint saying where to find it.
 */
export default function AddUserForm({ onAdded }: { onAdded: () => void }) {
  const [subject, setSubject] = useState("");
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await createUser(subject.trim(), { email: email.trim(), name: name.trim() });
      setSubject("");
      setEmail("");
      setName("");
      onAdded();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/15">
      <h2 className="text-sm font-medium">Add a person</h2>
      <div className="grid gap-3 sm:grid-cols-3">
        <Field
          label="OIDC subject"
          hint="The `sub` your identity provider issues them — copy it from its admin console."
        >
          <input
            className={INPUT}
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            required
            placeholder="auth0|abc123"
          />
        </Field>
        <Field label="Email">
          <input
            className={INPUT}
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
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
      </div>
      {error && <p className="text-xs text-red-600 dark:text-red-400">{error}</p>}
      <div>
        {/* A real submit rather than the shared PrimaryButton, which is a
            type="button" — inside a form, Enter should add the person. */}
        <button
          type="submit"
          disabled={busy}
          className="self-start rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
        >
          {busy ? "Adding…" : "Add"}
        </button>
      </div>
    </form>
  );
}
