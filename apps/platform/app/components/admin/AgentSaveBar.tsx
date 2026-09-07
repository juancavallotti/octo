"use client";

import { PrimaryButton } from "./fields";
import { useAgentForm } from "./AgentSettingsForm";
import {
  saveLlmSettings,
  saveWebSearchSettings,
} from "@/app/model/siteSettings";
import { setAgentDeploymentSettings } from "@/app/model/agent";

/**
 * The one Save.
 *
 * It writes only what changed, in a deliberate order: the provider first,
 * because it is the thing the agent cannot run without and the thing a roll-out
 * would carry; then the search key; then the two settings that live on the pods,
 * in a single call so they replace the pods once rather than twice.
 *
 * Saying what will happen matters more here than under the buttons this
 * replaced. "Apply" beside the turn limit was self-evidently about the
 * deployment; "Save" at the foot of a page is not, so when a pod-level field is
 * dirty the button says so before it is pressed rather than after.
 */
export default function AgentSaveBar() {
  const { draft, dirty, stored, busy, error, run } = useAgentForm();
  if (!draft) return null;

  // Pod settings can only reach something that exists. Changed while the agent
  // is not deployed, they stay in the draft and travel with the install instead
  // of being sent to an orchestrator that would refuse them.
  const deployed = Boolean(stored.status?.deploymentId);
  const rolls = dirty.deployment && deployed;

  const save = () =>
    run(async () => {
      if (dirty.llm) {
        await saveLlmSettings({
          provider: draft.provider,
          model: draft.model.trim(),
          ...(draft.llmApiKey ? { apiKey: draft.llmApiKey } : {}),
        });
      }
      if (dirty.webSearch) {
        await saveWebSearchSettings({ apiKey: draft.webSearchApiKey });
      }
      if (rolls) {
        // Together, so the pods are replaced once. An empty turn limit sends
        // zero, which the orchestrator reads as "no override" — the only way
        // back to the definition's own default.
        await setAgentDeploymentSettings({
          maxIterations: Number(draft.maxIterations.trim() || "0"),
          autoFix: draft.autoFix,
        });
      }
    });

  return (
    <div className="mt-4 flex flex-wrap items-center gap-3">
      <PrimaryButton onClick={save} disabled={!dirty.any || busy}>
        {busy ? "Saving…" : "Save"}
      </PrimaryButton>
      {dirty.any && !busy && (
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          {rolls
            ? "Saving replaces the agent’s pods: the runtime reads these when it starts."
            : "Unsaved changes."}
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-red-500">
          {error}
        </p>
      )}
    </div>
  );
}
