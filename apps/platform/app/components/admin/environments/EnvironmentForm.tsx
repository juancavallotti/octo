"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import type { EnvironmentInput } from "@/app/model/environments";
import { Field, INPUT } from "../fields";
import { defaultNamespace, nameProblem } from "./names";

/**
 * Create an environment. The namespace is optional and shown with the default
 * it will take, since that is the one choice that cannot be changed afterwards.
 * `onCreate` resolves true when the environment was made, which clears the form.
 */
export default function EnvironmentForm({
  busy,
  taken,
  onCreate,
}: {
  busy: boolean;
  taken: (name: string) => boolean;
  onCreate: (input: EnvironmentInput) => Promise<boolean>;
}) {
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [namespace, setNamespace] = useState("");
  const [description, setDescription] = useState("");

  const problem = nameProblem(name) ?? (taken(name) ? "That environment already exists." : null);
  const canCreate = !busy && name !== "" && problem === null;

  const submit = async () => {
    if (!canCreate) return;
    const made = await onCreate({
      name,
      displayName: displayName.trim() || undefined,
      namespace: namespace.trim() || undefined,
      description: description.trim() || undefined,
    });
    if (made) {
      setName("");
      setDisplayName("");
      setNamespace("");
      setDescription("");
    }
  };

  return (
    <form
      aria-label="New environment"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      className="mt-4 grid gap-3 rounded-lg border border-black/10 p-3 sm:grid-cols-2 dark:border-white/10"
    >
      <Field label="Name" hint={problem ?? "Used in the API, the CLI and the namespace."}>
        <input
          value={name}
          disabled={busy}
          placeholder="staging"
          onChange={(e) => setName(e.target.value.toLowerCase())}
          className={`${INPUT} font-mono`}
        />
      </Field>
      <Field label="Display name">
        <input
          value={displayName}
          disabled={busy}
          placeholder="Staging"
          onChange={(e) => setDisplayName(e.target.value)}
          className={INPUT}
        />
      </Field>
      <Field label="Namespace" hint="Fixed once created.">
        <input
          value={namespace}
          disabled={busy}
          placeholder={defaultNamespace(name)}
          onChange={(e) => setNamespace(e.target.value.toLowerCase())}
          className={`${INPUT} font-mono`}
        />
      </Field>
      <Field label="Description">
        <input
          value={description}
          disabled={busy}
          onChange={(e) => setDescription(e.target.value)}
          className={INPUT}
        />
      </Field>
      <div className="sm:col-span-2">
        <button
          type="submit"
          disabled={!canCreate}
          className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
        >
          <Plus size={14} />
          Create environment
        </button>
      </div>
    </form>
  );
}
