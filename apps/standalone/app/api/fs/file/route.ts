import { NextResponse } from "next/server";
import { readFlow, writeFlow } from "../store";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/fs/file?path=<id> — read one flow ({ id, name, definition }). */
export async function GET(req: Request) {
  const id = new URL(req.url).searchParams.get("path");
  if (!id) {
    return NextResponse.json({ error: "missing `path`" }, { status: 400 });
  }
  try {
    return NextResponse.json(await readFlow(id));
  } catch (err) {
    return NextResponse.json({ error: (err as Error).message }, { status: 404 });
  }
}

/** PUT /api/fs/file?path=<id> { definition } — overwrite an existing flow. */
export async function PUT(req: Request) {
  const id = new URL(req.url).searchParams.get("path");
  if (!id) {
    return NextResponse.json({ error: "missing `path`" }, { status: 400 });
  }
  let definition: unknown;
  try {
    definition = (await req.json())?.definition;
  } catch {
    return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  }
  if (typeof definition !== "string") {
    return NextResponse.json({ error: "missing `definition`" }, { status: 400 });
  }
  try {
    return NextResponse.json(await writeFlow(id, definition));
  } catch (err) {
    return NextResponse.json({ error: (err as Error).message }, { status: 400 });
  }
}
