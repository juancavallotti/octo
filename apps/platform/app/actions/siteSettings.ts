"use server";

/**
 * Server actions for site-wide settings — the admin section's data layer. Each
 * action authorizes and delegates to the orchestrator client lib; the model
 * unwraps the ActionResult.
 *
 * Administrators only, and the reads matter as much as the writes here: these
 * settings hold the installation's SMTP credentials and its LLM API keys, so who
 * may look is the same question as who may change.
 *
 * The embedding status is the exception, and deliberately not gated. It is a
 * health fact with nothing configurable behind it — the provider, model and key
 * are chart values on that server — and the memory search page shows it to
 * whoever is looking at their own memories.
 *
 * The test send is a write, not a read: it spends an external resource and can
 * carry an API key out of the browser.
 */

import { withAdmin, withRead } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";
import type {
  EmailSettings,
  EmailSettingsInput,
  LlmSettings,
  LlmSettingsInput,
  EmbeddingStatus,
  SentMessage,
  TestEmailInput,
  WebSearchSettings,
  WebSearchSettingsInput,
} from "./client/settings";

export async function getEmailSettings(): Promise<ActionResult<EmailSettings>> {
  return withAdmin(() => client.getEmailSettings());
}

export async function saveEmailSettings(
  input: EmailSettingsInput,
): Promise<ActionResult<EmailSettings>> {
  return withAdmin(() => client.saveEmailSettings(input));
}

export async function sendTestEmail(
  input: TestEmailInput,
): Promise<ActionResult<SentMessage>> {
  return withAdmin(() => client.sendTestEmail(input));
}

export async function getLlmSettings(): Promise<ActionResult<LlmSettings>> {
  return withAdmin(() => client.getLlmSettings());
}

export async function saveLlmSettings(
  input: LlmSettingsInput,
): Promise<ActionResult<LlmSettings>> {
  return withAdmin(() => client.saveLlmSettings(input));
}

export async function getWebSearchSettings(): Promise<
  ActionResult<WebSearchSettings>
> {
  return withAdmin(() => client.getWebSearchSettings());
}

export async function saveWebSearchSettings(
  input: WebSearchSettingsInput,
): Promise<ActionResult<WebSearchSettings>> {
  return withAdmin(() => client.saveWebSearchSettings(input));
}

/**
 * The embedding server's status. A read, and the only embedding call there is:
 * the provider, model and key are chart values on that server, so there is
 * nothing here to write.
 */
export async function getEmbeddingStatus(): Promise<
  ActionResult<EmbeddingStatus>
> {
  return withRead(() => client.getEmbeddingStatus());
}
