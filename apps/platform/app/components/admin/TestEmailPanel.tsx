"use client";

import { useState } from "react";
import { sendTestEmail, type EmailSettingsInput } from "@/app/model/siteSettings";
import { Field, INPUT, PrimaryButton } from "./fields";

/**
 * Sends a test message using the draft currently on screen, so a key can be checked
 * before it is saved — which is the order anyone actually configures this in. When
 * no key has been typed the orchestrator falls back to the stored one, so the button
 * still works after a reload.
 *
 * The recipient is not prefilled from the session: the local dev session has no
 * email address, and you often want to test against a different one anyway.
 */
export default function TestEmailPanel({
  draft,
  busy,
  configured,
  hasTypedKey,
}: {
  /** The current form draft, read at click time rather than captured on render. */
  draft: () => EmailSettingsInput;
  busy: boolean;
  configured: boolean;
  hasTypedKey: boolean;
}) {
  const [to, setTo] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sentTo, setSentTo] = useState<string | null>(null);

  // Without a key on either side there is nothing to send with. The orchestrator
  // rejects it too (409) — this just avoids a round trip to be told so.
  const haveKey = configured || hasTypedKey;
  const canSend = !busy && !sending && haveKey && to.trim().length > 0;

  const send = async () => {
    if (!canSend) return;
    setSending(true);
    setError(null);
    setSentTo(null);
    try {
      await sendTestEmail({ ...draft(), to: to.trim() });
      setSentTo(to.trim());
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="mt-4 flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/10">
      <div>
        <h2 className="text-sm font-semibold">Send a test</h2>
        <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
          Uses whatever is in the form above, including a key you have not saved yet.
        </p>
      </div>

      {error && <p className="text-sm text-red-500">{error}</p>}
      {sentTo && !error && (
        <p className="text-sm text-emerald-600 dark:text-emerald-400">
          Test message sent to {sentTo}.
        </p>
      )}

      <Field label="Send to">
        <input
          value={to}
          disabled={busy || sending}
          placeholder="you@example.com"
          onChange={(e) => setTo(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && send()}
          className={`${INPUT} w-full`}
        />
      </Field>

      {!haveKey && (
        <span className="text-xs text-zinc-500">
          Enter an API key above, or save one, before sending a test.
        </span>
      )}

      <PrimaryButton onClick={send} disabled={!canSend}>
        {sending ? "Sending…" : "Send test email"}
      </PrimaryButton>
    </div>
  );
}
