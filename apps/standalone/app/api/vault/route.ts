import { NextResponse } from "next/server";
import path from "node:path";
import { fsRoot } from "../fs/store";

/**
 * GET /api/vault — which directory is this editor serving?
 *
 *   { "name": "flows" }
 *
 * The name only, never the absolute path: this server has no authentication and may
 * be reachable off the machine, and the path carries the user's account name and
 * directory layout.
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const root = fsRoot();
  // basename("/") is the empty string, and a filesystem root is a legitimate if
  // eccentric thing to point OCTO_FS_DIR at.
  const name = path.basename(root) || path.parse(root).root;
  return NextResponse.json(
    { name },
    { headers: { "cache-control": "no-store" } },
  );
}
