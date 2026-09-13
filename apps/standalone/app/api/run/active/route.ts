import { NextResponse } from "next/server";
import { allSessions } from "../../../run/session";

/**
 * GET /api/run/active — how many flows are running right now?
 *
 *   { "running": 2, "namespaces": ["a1b2c3d4", "e5f6a7b8"] }
 *
 * A count, not a listing of what the runs are: it answers "is there work in flight
 * that stopping this server would throw away?" and nothing more. The namespaces come
 * along because they make the answer debuggable; they are opaque per-tab slugs, not
 * user data.
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
