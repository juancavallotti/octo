"use client";

import { useCallback, useEffect, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  getWebSearchSettings,
  saveWebSearchSettings,
  type WebSearchSettings,
} from "@/app/model/siteSettings";
import { ApiKeyField, EncryptionWarning, PrimaryButton } from "./fields";

/**
 * The key Dr. Octo searches the open web with.
 *
 * One field, because there is one decision: whether he can search at all. The
 * provider is Parallel and is not a choice — his tool is a parallel-search block,
 * so a second provider would be a second tool rather than a different value here.
 *
 * It sits below the LLM provider and above the deployment, in the order things are
 * needed: he cannot run without a model, he runs perfectly well without this. That
 * is the whole reason this section says what happens when it is empty — an operator
 * who reads "no key stored" should know it costs one tool, not the agent.
 *
 * As with the other two forms the key draft is never seeded from the server, and an
 * empty draft means "keep the stored key".
 */
export default function WebSearchSettingsManager() {
  const confirm = useConfirm();
  const [settings, setSettings] = useState<WebSearchSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const [apiKey, setApiKey] = useState("");

  // A promise chain rather than an async body, so nothing sets state in the
  // synchronous part of the effect below — the form the other managers use.
  const load = useCallback(
    () =>
      getWebSearchSettings().then(setSettings, (e) =>
        setError((e as Error).message),
      ),
    [],
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

  // Gated on the settings having loaded and on there being something to send: a
  // save with an empty draft is a write that changes nothing, and the button
  // offering it reads as though it would store what was typed.
  const canSave = settings !== null && !busy && apiKey.trim().length > 0;

  const save = () => {
    if (!canSave) return;
    run(async () => {
      await saveWebSearchSettings({ apiKey });
      setApiKey("");
      setSaved(true);
    });
  };

  const removeKey = async () => {
    if (settings === null || busy) return;
    const ok = await confirm({
      title: "Remove the stored API key?",
      body: "Dr. Octo keeps running, but he stops being able to search the web: the tool answers that it is not configured until a new key is saved and he is rolled out.",
      confirmLabel: "Remove",
      danger: true,
    });
    if (!ok) return;
    run(async () => {
      await saveWebSearchSettings({ apiKey: "" });
      setApiKey("");
    });
  };

  const encryptionAvailable = settings?.encryptionAvailable ?? true;
  const configured = settings?.configured ?? false;

  return (
    <section aria-labelledby="websearch-heading" className="mt-5">
      <h3 id="websearch-heading" className="text-sm font-semibold">
        Web search
      </h3>
      <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
        A{" "}
        <a
          href="https://parallel.ai"
          target="_blank"
          rel="noreferrer"
          className="underline underline-offset-2"
        >
          Parallel
        </a>{" "}
        key lets him look things up on the open web &mdash; a provider&apos;s
        error message, a version, what a release changed. Optional: without one
        he runs exactly as before and his search tool reports itself
        unavailable, so he answers from this installation and says he could not
        check.
      </p>

      {!encryptionAvailable && <EncryptionWarning />}
      {error && <p className="mt-3 text-sm text-red-500">{error}</p>}
      {saved && !error && (
        <p className="mt-3 text-sm text-emerald-600 dark:text-emerald-400">
          Settings saved. Roll him out below for it to reach him.
        </p>
      )}

      <div className="mt-4 flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/10">
        <ApiKeyField
          value={apiKey}
          onChange={setApiKey}
          configured={configured}
          last4={settings?.last4 ?? ""}
          disabled={busy || !encryptionAvailable}
          placeholder="Your Parallel API key"
          onRemove={removeKey}
        />

        <PrimaryButton onClick={save} disabled={!canSave} />

        {/* The key is read when the deployment's bindings are written, which is
            install and roll-out and nothing else. Saying so here is the
            difference between "it does not work" and "it has not reached him
            yet" — the one question this form would otherwise generate. */}
        <p className="text-xs text-zinc-500">
          The key travels to him as a cluster secret when he is installed or
          rolled out. Change it while he is running and it takes effect on his
          next roll-out.
        </p>
      </div>
    </section>
  );
}
