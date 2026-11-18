"use server";

/**
 * Server actions backing the editor's certificate capability: self-signed
 * certificates minted with this server's `openssl`. Gated like any write, since
 * the minted pair is about to be stored as an integration's resources.
 */

import {
  mintSelfSigned,
  opensslAvailable,
  type SelfSignedPair,
  type SelfSignedRequest,
} from "@octo/run-host";
import { withWrite } from "./_auth";
import type { ActionResult } from "./_client";

/** Whether this server can mint certificates. */
export async function certificatesAvailable(): Promise<ActionResult<boolean>> {
  return withWrite(async () => ({ ok: true, data: await opensslAvailable() }));
}

/** Mint a self-signed certificate. */
export async function mintCertificate(
  req: SelfSignedRequest,
): Promise<ActionResult<SelfSignedPair>> {
  return withWrite(async () => {
    try {
      return { ok: true, data: await mintSelfSigned(req) };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}
