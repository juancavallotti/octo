import { NextResponse } from "next/server";
import path from "node:path";
import { fsRoot } from "../fs/store";

/**
 * GET /api/vault — which directory is this editor serving?
 *
 *   { "name": "flows" }
 *
 * The store root is a server-side fact (OCTO_FS_DIR, resolved by `fsRoot()`), and
 * until now nothing told the browser what it was. Every deployment of the standalone
 * app wants to say so: the Docker image is serving a bind mount the user chose, and
 * the desktop shell is serving a folder the user picked from a native dialog. Both
 * are "which vault am I in?", and a route answers it for both — which is why this is
 * here rather than in the desktop app's IPC bridge, where it would only serve one of
 * them.
 *
 * The NAME only, never the absolute path. "Local-only" is not quite true of the
 * Docker image: it binds 0.0.0.0 and publishes its port with no authentication, so
 * on a shared network the path — which carries the user's account name and their
 * directory layout — is readable by anyone who can reach the editor. The folder's
 * name is all this is for, and all that any browser needs to display it.
 *
 * The desktop app shows the full path in a tooltip and gets it from its own IPC
 * bridge (octo:vault:get), where the answer never leaves the machine.
 */
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const root = fsRoot();
  // basename("/") is the empty string, and a filesystem root is a legitimate if
  // eccentric thing to point OCTO_FS_DIR at — a bind mount of "/" in the Docker
  // image reaches here. An empty name renders as a nameless chip in the header,
  // so fall back to the root itself.
  const name = path.basename(root) || path.parse(root).root;
  return NextResponse.json(
    { name },
    { headers: { "cache-control": "no-store" } },
  );
}
