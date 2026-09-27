/**
 * The files a message carries to Dr. Octo.
 *
 * They cross as base64 on the request body, because that is the shape the
 * runtime reads them in: an ai-agent's `attachments` setting is a CEL
 * expression, CEL has no bytes type, and a string is what survives the trip
 * through JSON.
 *
 * What is accepted is the provider's business and the four providers disagree,
 * so the set is reported by the status route rather than decided here. ACCEPTED
 * below is only the fallback for a site that cannot say.
 *
 * Not a client module, though most of it runs in the browser: the limits are
 * also what the chat route refuses a forged body against, and a `"use client"`
 * directive here would put a client boundary in the middle of a server route.
 */

import { toBase64 } from "@/app/model/base64";
import { readFileBytes } from "@/app/components/integrations/files";
import { randomId } from "./thread";

export interface Attachment {
  /** Local identity, for React keys and for removing one. Never sent. */
  id: string;
  name: string;
  mimeType: string;
  /** The file, base64. */
  data: string;
  /** The decoded size, for the chip and for the total-size check. */
  size: number;
}

/**
 * The limits, and why these numbers.
 *
 * Base64 inflates by about a third, so five four-megabyte files would be roughly
 * eleven megabytes of JSON — which is what the agent's HTTP source is sized for.
 * They also sit under every provider's own per-file and per-request caps, so a
 * file this accepts is one the model will accept.
 */
export const MAX_FILE_BYTES = 4 * 1024 * 1024;
export const MAX_TOTAL_BYTES = 8 * 1024 * 1024;
export const MAX_FILES = 5;

/**
 * What to offer when the status route cannot say what the site's model reads.
 *
 * The **intersection** of the four providers' sets, deliberately, not the union:
 * guessing wide would offer a file that is refused on the first turn, and the
 * whole point of asking the server is to not do that.
 */
export const ACCEPTED = [
  "image/png",
  "image/jpeg",
  "image/webp",
  "application/pdf",
] as const;

/**
 * Whether `accepted` covers this content type, matching a `type/` entry against
 * the whole family and anything else exactly.
 *
 * The same rule the Gemini connector applies to its own list, and deliberately so:
 * that connector accepts `video/` by prefix, so exact matching here would turn
 * away a .mov the model would have read.
 */
export function sendable(mime: string, accepted: readonly string[]): boolean {
  return accepted.some((entry) => (entry.endsWith("/") ? mime.startsWith(entry) : mime === entry));
}

/**
 * The accept attribute for a file input.
 *
 * A family is written `video/*`, which is the spelling a browser understands —
 * the table holds `video/` because that is the runtime's spelling, and this is the
 * one place the two differ.
 */
export function acceptAttribute(accepted: readonly string[]): string {
  return accepted.map((entry) => (entry.endsWith("/") ? `${entry}*` : entry)).join(",");
}

/** Render a size the way a chip should show it. */
export function humanSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Whether `file` may join `current`, or the sentence saying why not.
 *
 * It returns the reason rather than a boolean because every rejection here has
 * to be shown: a file that vanishes on drop reads as a broken panel.
 */
export function acceptable(
  file: File,
  current: Attachment[],
  accepted: readonly string[],
): string | null {
  if (accepted.length === 0) {
    return "This model does not accept attachments.";
  }
  if (!sendable(file.type, accepted)) {
    return `${file.name}: ${file.type || "that kind of file"} is not accepted here.`;
  }
  if (file.size > MAX_FILE_BYTES) {
    return `${file.name} is ${humanSize(file.size)}; the limit is ${humanSize(MAX_FILE_BYTES)}.`;
  }
  if (current.length >= MAX_FILES) {
    return `Up to ${MAX_FILES} files per message.`;
  }
  const total = current.reduce((sum, a) => sum + a.size, 0);
  if (total + file.size > MAX_TOTAL_BYTES) {
    return `That would be more than ${humanSize(MAX_TOTAL_BYTES)} in one message.`;
  }
  return null;
}

/** Read one picked file into the shape the request carries. */
export async function readAttachment(file: File): Promise<Attachment> {
  const bytes = await readFileBytes(file);
  return {
    id: randomId(),
    name: file.name,
    mimeType: file.type,
    data: toBase64(bytes),
    size: bytes.length,
  };
}

/** The filenames, for a transcript turn that should name its files and not hold them. */
export function attachmentNames(attachments: Attachment[]): string[] {
  return attachments.map((a) => a.name);
}

/**
 * What goes on the wire: everything but the local id and the decoded size, which
 * the runtime neither reads nor needs.
 */
export function wireAttachments(
  attachments: Attachment[],
): { name: string; mimeType: string; data: string }[] {
  return attachments.map(({ name, mimeType, data }) => ({ name, mimeType, data }));
}
