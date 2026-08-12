/**
 * Browser-side client for the admin section's site-wide settings. Backed by the
 * server actions in `app/actions/siteSettings.ts`; these wrappers unwrap the
 * ActionResult so callers keep a value-or-throw contract.
 *
 * API keys are write-only: nothing here reads one back, because the orchestrator
 * has no path that returns it. A stored key surfaces only as `configured` and
 * `last4`.
 */

import * as actions from "@/app/actions/siteSettings";
import { unwrap } from "./bff";

export type {
  EmailSettings,
  EmailSettingsInput,
  LlmSettings,
  LlmSettingsInput,
  SentMessage,
  TestEmailInput,
} from "@/app/actions/client/settings";

import type {
  EmailSettings,
  EmailSettingsInput,
  LlmSettings,
  LlmSettingsInput,
  SentMessage,
  TestEmailInput,
} from "@/app/actions/client/settings";

/** Read the site's email settings. Never includes the API key. */
export async function getEmailSettings(): Promise<EmailSettings> {
  return unwrap(await actions.getEmailSettings());
}

/**
 * Save the email settings. Omit `apiKey` to keep the stored key; pass "" to
 * remove it.
 */
export async function saveEmailSettings(
  input: EmailSettingsInput,
): Promise<EmailSettings> {
  return unwrap(await actions.saveEmailSettings(input));
}

/**
 * Send a test message using the supplied draft, falling back to the stored key
 * when none is supplied.
 */
export async function sendTestEmail(input: TestEmailInput): Promise<SentMessage> {
  return unwrap(await actions.sendTestEmail(input));
}

/** Read the site's LLM provider settings. Never includes the API key. */
export async function getLlmSettings(): Promise<LlmSettings> {
  return unwrap(await actions.getLlmSettings());
}

/** Save the LLM provider settings. `apiKey` behaves as in {@link saveEmailSettings}. */
export async function saveLlmSettings(
  input: LlmSettingsInput,
): Promise<LlmSettings> {
  return unwrap(await actions.saveLlmSettings(input));
}
