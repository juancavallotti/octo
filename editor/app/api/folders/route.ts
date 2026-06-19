import { proxy } from "../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/folders — the folder tree. */
export function GET(req: Request) {
  return proxy(req, "/folders");
}

/** POST /api/folders { name, parentId } — create a folder. */
export function POST(req: Request) {
  return proxy(req, "/folders");
}
