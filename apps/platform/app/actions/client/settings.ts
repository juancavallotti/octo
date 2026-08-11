/**
 * Site-wide settings operations: the email provider the platform sends through,
 * and the LLM provider its agent reasons with.
 *
 * Neither response ever carries an API key — the orchestrator returns `configured`
 * and `last4` and nothing more — so there is deliberately no "get key" operation
 * here to call.
 */

import type { ActionResult } from "@octo/http";
import { call } from "./http";

/** The site's email settings, as the orchestrator reports them. */
export interface EmailSettings {
  fromEmail: string;
  fromName: string;
  replyTo: string;
  /** Whether an API key is stored. The key itself is never returned. */
  configured: boolean;
  /** Last 4 characters of the stored key, for recognition. "" when unset. */
  last4: string;
  updatedAt: string | null;
  /** False when the orchestrator has no encryption key, so storing one is refused. */
  encryptionAvailable: boolean;
}

/**
 * One email settings save. `apiKey` is optional rather than nullable: omitting the
 * property keeps the stored key, and `JSON.stringify` drops an undefined property
 * entirely, which the orchestrator decodes as "not supplied". Passing "" removes it.
 */
export interface EmailSettingsInput {
  apiKey?: string;
  fromEmail: string;
  fromName: string;
  replyTo: string;
}

/** A test send: the draft on screen, plus who to send it to. */
export interface TestEmailInput extends EmailSettingsInput {
  to: string;
}

/** The site's LLM provider settings. */
export interface LlmSettings {
  provider: string;
  model: string;
  configured: boolean;
  last4: string;
  updatedAt: string | null;
  encryptionAvailable: boolean;
}

/** One LLM settings save. `apiKey` behaves as in {@link EmailSettingsInput}. */
export interface LlmSettingsInput {
  apiKey?: string;
  provider: string;
  model: string;
}

/** The provider's id for a queued message. */
export interface SentMessage {
  messageId: string;
}

export function getEmailSettings(): Promise<ActionResult<EmailSettings>> {
  return call<EmailSettings>("GET", "/settings/email");
}

export function saveEmailSettings(
  input: EmailSettingsInput,
): Promise<ActionResult<EmailSettings>> {
  return call<EmailSettings>("PUT", "/settings/email", input);
}

export function sendTestEmail(
  input: TestEmailInput,
): Promise<ActionResult<SentMessage>> {
  return call<SentMessage>("POST", "/settings/email/test", input);
}

export function getLlmSettings(): Promise<ActionResult<LlmSettings>> {
  return call<LlmSettings>("GET", "/settings/llm");
}

export function saveLlmSettings(
  input: LlmSettingsInput,
): Promise<ActionResult<LlmSettings>> {
  return call<LlmSettings>("PUT", "/settings/llm", input);
}
