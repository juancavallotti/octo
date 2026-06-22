import { NextResponse } from "next/server";
import { forward, orchestratorConfigured } from "./client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/**
 * GET /api/orchestrator — whether integration features are available. True only
 * when ORCHESTRATOR_URL is set and the orchestrator answers its health check.
 * Mirrors how RunContext probes /api/run for runner availability.
 */
export async function GET() {
  if (!orchestratorConfigured()) {
    return NextResponse.json({ available: false });
  }
  const res = await forward("/healthz");
  return NextResponse.json({ available: res.ok });
}
