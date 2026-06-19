import { proxy } from "../../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** PUT /api/folders/:id { name, parentId } — rename / re-parent a folder. */
export async function PUT(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/folders/${encodeURIComponent(id)}`);
}

/** DELETE /api/folders/:id — delete a folder. */
export async function DELETE(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/folders/${encodeURIComponent(id)}`);
}
