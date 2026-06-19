import { NextResponse } from "next/server";
import { status } from "./session";
import { probeVersion } from "./version";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/run — whether RUN is available, whether a runner is live, and its version. */
export async function GET() {
  await probeVersion(); // warm the version cache so status() can read it
  return NextResponse.json(status());
}
