import { proxy } from "../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/secrets — list cluster secrets (names + timestamps, never values). */
export async function GET(req: Request) {
  return proxy(req, "/secrets");
}
