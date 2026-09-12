"use client";

import { useEffect, useState } from "react";
import { UserPlus, UserPen, X } from "lucide-react";
import {
  createUser,
  grantRole,
  revokeRole,
  updateUser,
  type PlatformUser,
  type RoleOption,
} from "@/app/model/users";
import { Field, INPUT } from "../fields";
import RolePicker from "./RolePicker";

/**
 * Adding a person, and editing one. One dialog because they are one form: an
 * address, a name, and what that person may do.
 *
 * Roles live here and nowhere else. They used to be chips in the table, which
 * made granting one a single click on a list somebody was reading — the way an
 * administrator hands out `platform:admin` while meaning to scroll. A dialog is
 * the difference between a decision and a slip.
 *
 * `person` absent means adding. Adding sends the roles with the create, so that
 * a person never exists here holding something nobody chose; editing sends the
 * profile and then the difference in roles, because those are the operations iam
 * offers and each answers with the whole user.
 */
export default function UserModal({
  person,
  catalogue,
  isSelf,
  onSaved,
  onClose,
}: {
  /** The person being edited, or undefined when adding one. */
  person?: PlatformUser;
  catalogue: RoleOption[];
  /**
   * Whether this row is the caller's own. Editing your own roles is how somebody
   * locks themselves out of the section they are standing in; iam refuses to
   * remove the last administrator regardless, and this stops the attempt being
   * made by accident.
   */
  isSelf?: boolean;
  onSaved: () => void;
  onClose: () => void;
}) {
  const editing = person !== undefined;
  const [email, setEmail] = useState(person?.email ?? "");
  const [name, setName] = useState(person?.name ?? "");
  const [roles, setRoles] = useState<string[]>(person?.roles ?? []);
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
      if (person) {
        await updateUser(person.id, { email: email.trim(), name: name.trim() });
        // One at a time, and each answers with the whole user: two in flight
        // together can land out of order and leave the row showing a set nobody
        // asked for.
        for (const role of roles.filter((r) => !person.roles.includes(r))) {
          await grantRole(person.id, role);
        }
        for (const role of person.roles.filter((r) => !roles.includes(r))) {
          await revokeRole(person.id, role);
        }
      } else {
        await createUser({ email: email.trim(), name: name.trim(), roles });
      }
      onSaved();
      onClose();
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  const title = editing ? `Edit ${person.name || person.email}` : "Add a person";
  const Icon = editing ? UserPen : UserPlus;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onMouseDown={() => !busy && onClose()}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm"
    >
      <form
        onSubmit={submit}
        onMouseDown={(e) => e.stopPropagation()}
        className="flex max-h-full w-full max-w-md flex-col overflow-hidden rounded-xl border border-black/10 bg-white shadow-xl dark:border-white/10 dark:bg-zinc-900"
      >
        <header className="flex items-center gap-2 border-b border-black/10 px-4 py-3 dark:border-white/10">
          <Icon size={16} className="text-sky-500" />
          <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">{title}</h3>
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

        <div className="flex min-h-0 flex-col gap-3 overflow-y-auto px-4 py-4">
          <Field
            label="Email"
            hint={
              editing
                ? "Correcting this does not move the account: their sign-in keys on the subject once they have one."
                : "The address they sign in with. Their first sign-in claims this account."
            }
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
          <RolePicker
            catalogue={catalogue}
            held={roles}
            disabled={isSelf}
            reason={
              isSelf ? "You cannot change your own roles." : undefined
            }
            onChange={setRoles}
          />
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
              type="button" — inside a form, Enter should save. */}
          <button
            type="submit"
            disabled={busy || !email.trim()}
            className="rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
          >
            {busy ? "Saving…" : editing ? "Save" : "Add"}
          </button>
        </footer>
      </form>
    </div>
  );
}
