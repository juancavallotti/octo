"use client";

import { PrimaryButton } from "./fields";
import { useAgentForm } from "./AgentSettingsForm";
import {
  saveLlmSettings,
  saveWebSearchSettings,
} from "@/app/model/siteSettings";
import { setAgentDeploymentSettings } from "@/app/model/agent";
import { turnLimitError } from "./AgentTurnLimit";

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

  // Rendered disabled rather than not rendered while the page is still loading:
  // a button that appears once the fetch lands moves everything under it, and
  // "there is nothing to save yet" is what disabled already means.
  // The one place an error from this page is shown, whether it came from a load,
  // an action in a section, or this save. Two surfaces reading one value printed
  // the same sentence twice.
  const message = error && (
    <p role="alert" className="text-sm text-red-500">
      {error}
    </p>
  );

  if (!draft) {
    return (
      <div className="mt-4 flex flex-wrap items-center gap-3">
        <PrimaryButton onClick={() => {}} disabled>
          Save
        </PrimaryButton>
        {message}
      </div>
    );
  }

  // Pod settings can only reach something that exists. Changed while the agent
  // is not deployed, they stay in the draft and travel with the install instead
  // of being sent to an orchestrator that would refuse them.
  const deployed = Boolean(stored.status?.deploymentId);
  // What the server last told us, in the draft's own shape, so each field can be
  // compared with what is on screen.
  const current = {
    maxIterations: stored.status?.maxIterations
      ? String(stored.status.maxIterations)
      : "",
    autoFix: stored.status?.autoFix ?? false,
  };
  const rolls = dirty.deployment && deployed;

  // Validated here because the per-section buttons used to do it and something
  // still must: a global Save that writes an empty model, or a turn limit
  // outside the bounds, would fail at the orchestrator having already replaced
  // the pods to find out.
  const invalid =
    (dirty.llm && draft.model.trim() === "") ||
    (dirty.deployment && turnLimitError(draft.maxIterations) !== null);

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
        // Only the field that actually changed. The endpoint reads an omitted
        // field as "leave it alone", which exists for exactly this: sending both
        // whenever either is dirty would write a stale copy of the untouched one
        // over whatever another operator set since this page loaded.
        //
        // Still one call, so the pods are replaced once. An empty turn limit
        // sends zero, which the orchestrator reads as "no override" — the only
        // way back to the definition's own default.
        const stored = {
          maxIterations: current.maxIterations,
          autoFix: current.autoFix,
        };
        await setAgentDeploymentSettings({
          ...(draft.maxIterations.trim() !== stored.maxIterations
            ? { maxIterations: Number(draft.maxIterations.trim() || "0") }
            : {}),
          ...(draft.autoFix !== stored.autoFix
            ? { autoFix: draft.autoFix }
            : {}),
        });
      }
    });

  return (
    <div className="mt-4 flex flex-wrap items-center gap-3">
      <PrimaryButton onClick={save} disabled={!dirty.any || invalid || busy}>
        {busy ? "Saving…" : "Save"}
      </PrimaryButton>
      {dirty.any && !busy && (
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          {rolls
            ? "Saving replaces the agent’s pods: the runtime reads these when it starts."
            : "Unsaved changes."}
        </p>
      )}
      {message}
    </div>
  );
}
