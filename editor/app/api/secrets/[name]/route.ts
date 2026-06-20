import { proxy } from "../../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ name: string }> };

/** PUT /api/secrets/:name — create or overwrite a secret's value (write-only). */
export async function PUT(req: Request, { params }: Params) {
  const { name } = await params;
  return proxy(req, `/secrets/${encodeURIComponent(name)}`);
}

/** DELETE /api/secrets/:name — delete a secret; ?force=true overrides the in-use guard. */
export async function DELETE(req: Request, { params }: Params) {
  const { name } = await params;
  const force = new URL(req.url).searchParams.get("force") === "true";
  return proxy(
    req,
    `/secrets/${encodeURIComponent(name)}${force ? "?force=true" : ""}`,
  );
}
