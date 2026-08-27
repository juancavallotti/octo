"use server";

/**
 * Server actions for site-wide settings — the admin section's data layer. Each
 * action authorizes and delegates to the orchestrator client lib; the model
 * unwraps the ActionResult.
 *
 * The test send is a write, not a read: it spends an external resource and can
 * carry an API key out of the browser.
 */

import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";
import type {
  EmailSettings,
  EmailSettingsInput,
  LlmSettings,
  LlmSettingsInput,
  EmbeddingStatus,
  EmbeddingSettings,
  EmbeddingSettingsInput,
  SentMessage,
  TestEmailInput,
} from "./client/settings";

export async function getEmailSettings(): Promise<ActionResult<EmailSettings>> {
  return withRead(() => client.getEmailSettings());
}

export async function saveEmailSettings(
  input: EmailSettingsInput,
): Promise<ActionResult<EmailSettings>> {
  return withWrite(() => client.saveEmailSettings(input));
}

export async function sendTestEmail(
  input: TestEmailInput,
): Promise<ActionResult<SentMessage>> {
  return withWrite(() => client.sendTestEmail(input));
}

export async function getLlmSettings(): Promise<ActionResult<LlmSettings>> {
  return withRead(() => client.getLlmSettings());
}

export async function saveLlmSettings(
  input: LlmSettingsInput,
): Promise<ActionResult<LlmSettings>> {
  return withWrite(() => client.saveLlmSettings(input));
}

export async function getEmbeddingSettings(): Promise<ActionResult<EmbeddingStatus>> {
  return withRead(() => client.getEmbeddingSettings());
}

export async function saveEmbeddingSettings(
  input: EmbeddingSettingsInput,
): Promise<ActionResult<EmbeddingSettings>> {
  return withWrite(() => client.saveEmbeddingSettings(input));
}

export async function clearEmbeddingSettings(): Promise<ActionResult<void>> {
  return withWrite(() => client.clearEmbeddingSettings());
}
