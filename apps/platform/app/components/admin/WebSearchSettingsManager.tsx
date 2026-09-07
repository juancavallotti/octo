"use client";

import { useConfirm } from "@/app/components/ConfirmDialog";
import { saveWebSearchSettings } from "@/app/model/siteSettings";
import { ApiKeyField, EncryptionWarning } from "./fields";
import { useAgentForm } from "./AgentSettingsForm";

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
 *
 * Presentational: what is stored, what is being typed and when it is written are
 * all the page's, held in AgentSettingsForm. This renders one field and owns the
 * prose around it. Removing the key is the exception and stays here, because it
 * is destructive, it asks first, and a revocation deferred behind a Save that is
 * never pressed is a key somebody believes is gone.
 */
export default function WebSearchSettingsManager() {
  const confirm = useConfirm();
  const { stored, draft, set, busy, loading, run } = useAgentForm();
  const settings = stored.webSearch;
  const apiKey = draft?.webSearchApiKey ?? "";

  const removeKey = async () => {
    if (settings === null || busy) return;
    const ok = await confirm({
      title: "Remove the stored API key?",
      body: "Dr. Octo keeps running, but he stops being able to search the web: the tool answers that it is not configured until a new key is saved and he is rolled out.",
      confirmLabel: "Remove",
      danger: true,
    });
    if (!ok) return;
    await run(async () => {
      await saveWebSearchSettings({ apiKey: "" });
      set("webSearchApiKey", "");
    });
  };

  const encryptionAvailable = settings?.encryptionAvailable ?? true;
  const configured = settings?.configured ?? false;

  return (
    <section aria-labelledby="websearch-heading" className="p-5">
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

      <div className="mt-4 flex flex-col gap-3">
        <ApiKeyField
          value={apiKey}
          onChange={(v) => set("webSearchApiKey", v)}
          configured={configured}
          last4={settings?.last4 ?? ""}
          disabled={busy || loading || !encryptionAvailable}
          placeholder="Your Parallel API key"
          onRemove={removeKey}
        />

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
