"use client";

import { useCallback, useEffect, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  getEmailSettings,
  saveEmailSettings,
  type EmailSettings,
  type EmailSettingsInput,
} from "@/app/model/siteSettings";
import { ApiKeyField, EncryptionWarning, Field, INPUT, PrimaryButton } from "./fields";
import TestEmailPanel from "./TestEmailPanel";

/**
 * Site-wide email delivery settings: the Resend key plus the identity messages go
 * out under.
 *
 * The API key draft is held separately from the loaded settings and is never seeded
 * from the server — there is nothing to seed it with, because no path returns a
 * stored key. An empty draft means "keep the stored key", and the payload omits the
 * property entirely rather than sending an empty string, which the orchestrator
 * reads as "remove it".
 */

/** A from-address must be bare; the display name goes in its own field. */
const EMAIL_RE = /^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/;

export default function EmailSettingsManager() {
  const confirm = useConfirm();
  const [settings, setSettings] = useState<EmailSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const [fromEmail, setFromEmail] = useState("");
  const [fromName, setFromName] = useState("");
  const [replyTo, setReplyTo] = useState("");
  const [apiKey, setApiKey] = useState("");

  const apply = useCallback((next: EmailSettings) => {
    setSettings(next);
    setFromEmail(next.fromEmail);
    setFromName(next.fromName);
    setReplyTo(next.replyTo);
  }, []);

  // Shaped as a promise chain rather than an async body so nothing sets state in the
  // synchronous part of the effect below — the same form the other managers use.
  const load = useCallback(
    () => getEmailSettings().then(apply, (e) => setError((e as Error).message)),
    [apply],
  );

  useEffect(() => {
    load();
  }, [load]);

  /** Run a mutation, then refresh; surface failures inline. */
  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      setSaved(false);
      try {
        await fn();
        await load();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  /** The draft, with apiKey present only when one was typed. */
  const draft = (): EmailSettingsInput => ({
    fromEmail,
    fromName,
    replyTo,
    ...(apiKey ? { apiKey } : {}),
  });

  const canSave = !busy && EMAIL_RE.test(fromEmail.trim());

  const save = () => {
    if (!canSave) return;
    run(async () => {
      await saveEmailSettings(draft());
      setApiKey("");
      setSaved(true);
    });
  };

  const removeKey = async () => {
    const ok = await confirm({
      title: "Remove the stored API key?",
      body: "Email sending will stop working until a new key is saved.",
      confirmLabel: "Remove",
      danger: true,
    });
    if (!ok) return;
    run(async () => {
      await saveEmailSettings({ fromEmail, fromName, replyTo, apiKey: "" });
      setApiKey("");
    });
  };

  const encryptionAvailable = settings?.encryptionAvailable ?? true;

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h1 className="text-lg font-semibold">Email</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          How this installation sends email. Messages go out through Resend, from the
          address below — which has to be on a domain you have verified with them.
        </p>

        {!encryptionAvailable && <EncryptionWarning />}
        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}
        {saved && !error && (
          <p className="mt-3 text-sm text-emerald-600 dark:text-emerald-400">
            Settings saved.
          </p>
        )}

        <div className="mt-4 flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/10">
          <Field label="From address" hint="A bare address — the display name goes below.">
            <input
              value={fromEmail}
              disabled={busy}
              placeholder="notifications@example.com"
              onChange={(e) => setFromEmail(e.target.value)}
              className={`${INPUT} w-full`}
            />
          </Field>
          {fromEmail.trim().length > 0 && !EMAIL_RE.test(fromEmail.trim()) && (
            <span className="text-xs text-amber-500">
              Enter a bare email address, without a display name.
            </span>
          )}

          <Field label="From name" hint="Optional. Shown as the sender's name.">
            <input
              value={fromName}
              disabled={busy}
              placeholder="Acme Notifications"
              onChange={(e) => setFromName(e.target.value)}
              className={`${INPUT} w-full`}
            />
          </Field>

          <Field label="Reply-to" hint="Optional. Where replies should go instead.">
            <input
              value={replyTo}
              disabled={busy}
              placeholder="support@example.com"
              onChange={(e) => setReplyTo(e.target.value)}
              className={`${INPUT} w-full`}
            />
          </Field>

          <ApiKeyField
            value={apiKey}
            onChange={setApiKey}
            configured={settings?.configured ?? false}
            last4={settings?.last4 ?? ""}
            disabled={busy || !encryptionAvailable}
            placeholder="re_..."
            onRemove={removeKey}
          />

          <PrimaryButton onClick={save} disabled={!canSave} />
        </div>

        <TestEmailPanel
          draft={draft}
          busy={busy}
          configured={settings?.configured ?? false}
          hasTypedKey={apiKey.length > 0}
        />
      </div>
    </div>
  );
}
