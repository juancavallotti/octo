import { NextResponse } from "next/server";

/**
 * GET /api/health — is the server up and serving?
 *
 *   { "ok": true }
 *
 * The cheapest route in the app, meant to be polled: no filesystem, no child
 * process, no imports beyond the response. It answers whether the server is
 * listening and nothing else — whether the runner works is a separate question with
 * a separate answer.
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  return NextResponse.json(
    { ok: true },
    { headers: { "cache-control": "no-store" } },
  );
}
