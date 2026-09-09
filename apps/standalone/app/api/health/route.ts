import { NextResponse } from "next/server";

/**
 * GET /api/health — is the server up and serving?
 *
 *   { "ok": true }
 *
 * Deliberately the cheapest route in the app. The desktop shell polls this every
 * 100ms while its splash screen is up (apps/desktop/src/main/server.ts), and the
 * obvious alternative — probing `/` — is the wrong probe twice over: it renders the
 * editor page server-side, and that page calls `probeSchema()`, which execs
 * `octo schema`. That conflates "the server is listening" with "the runner works",
 * so a missing OCTO_BIN_PATH would read as a server that never came up, and every
 * poll would spawn a process.
 *
 * So: no filesystem, no child process, no imports beyond the response. Whether the
 * runner is available is a separate question the editor already answers elsewhere
 * (`binaries()` in @octo/run-host).
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  return NextResponse.json(
    { ok: true },
    { headers: { "cache-control": "no-store" } },
  );
}
