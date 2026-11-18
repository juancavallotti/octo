"use server";

/**
 * Server actions backing the editor's certificate capability: self-signed
 * certificates minted with the local machine's `openssl`.
 */

import type { ActionResult } from "@octo/http";
import {
  mintSelfSigned,
  opensslAvailable,
  type SelfSignedPair,
  type SelfSignedRequest,
} from "@octo/run-host";

/** Whether this machine can mint certificates. */
export async function certificatesAvailable(): Promise<ActionResult<boolean>> {
  return { ok: true, data: await opensslAvailable() };
}

/** Mint a self-signed certificate. */
export async function mintCertificate(
  req: SelfSignedRequest,
): Promise<ActionResult<SelfSignedPair>> {
  try {
    return { ok: true, data: await mintSelfSigned(req) };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
