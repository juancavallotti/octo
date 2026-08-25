/**
 * Base64 for the bytes that cross the server-action boundary — today the
 * integration bundles.
 *
 * A server action's arguments and result are serialized by React, so a binary
 * document travels as text rather than as bytes. One module for both directions,
 * built on `atob`/`btoa` (standard in the browser and in Node), so the server and
 * the browser cannot drift into two encodings that disagree at the edges.
 */

/** How many bytes are converted per `String.fromCharCode` call. Applying the
 * spread operator to a whole multi-megabyte array overflows the call stack, so
 * the conversion is chunked. */
const CHUNK = 0x8000;

/** Encode bytes as a base64 string. */
export function toBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }
  return btoa(binary);
}

/** Decode a base64 string back into bytes. */
export function fromBase64(encoded: string): Uint8Array {
  const binary = atob(encoded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}
