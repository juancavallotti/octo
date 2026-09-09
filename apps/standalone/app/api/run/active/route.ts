import { NextResponse } from "next/server";
import { allSessions } from "../../../run/session";

/**
 * GET /api/run/active — how many flows are running right now?
 *
 *   { "running": 2, "namespaces": ["a1b2c3d4", "e5f6a7b8"] }
 *
 * A count, not a listing of what they are: the caller is the desktop shell, and
 * the only question it asks is "am I about to throw away work the user forgot
 * about?" — it shows a confirmation before switching folders, which stops every
 * run in flight. Without this the shell would have to either never warn (and
 * silently kill a running integration) or always warn (and train the user to
 * click through it).
 *
 * Namespaces are included because they cost nothing and make the answer
 * debuggable; they are opaque per-tab slugs, not user data.
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const live = [...allSessions().entries()].filter(([, s]) => s.proc !== null);
  return NextResponse.json(
    { running: live.length, namespaces: live.map(([ns]) => ns) },
    { headers: { "cache-control": "no-store" } },
  );
}
