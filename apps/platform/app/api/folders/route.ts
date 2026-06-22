import { proxy } from "../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/folders — the folder tree. */
export function GET(req: Request) {
  return proxy(req, "/folders");
}

/** POST /api/folders { name, parentId } — create a folder. */
export const POST = withAuth((req: Request) => proxy(req, "/folders"), {
  roles: writeRoles,
});
