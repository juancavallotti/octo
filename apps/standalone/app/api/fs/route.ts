import { NextResponse } from "next/server";
import { createFlow, listFlows } from "./store";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/fs — list the stored flows ({ id, name }). */
export async function GET() {
  return NextResponse.json(await listFlows());
}

/** POST /api/fs { name, definition } — create a new flow file. */
export async function POST(req: Request) {
  let body: { name?: unknown; definition?: unknown };
  try {
    body = (await req.json()) ?? {};
  } catch {
    return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  }
  const name = typeof body.name === "string" ? body.name : "";
  const definition = body.definition;
  if (typeof definition !== "string") {
    return NextResponse.json({ error: "missing `definition`" }, { status: 400 });
  }
  try {
    return NextResponse.json(await createFlow(name, definition));
  } catch (err) {
    return NextResponse.json({ error: (err as Error).message }, { status: 400 });
  }
}
