import { NextResponse } from "next/server";
import { readFile } from "node:fs/promises";
import path from "node:path";

/**
 * Dev-only loader for the repo's `samples/*.yaml` flows, used by the `/preview`
 * route and the Playwright screenshot harness (see editor/e2e/screenshots.spec.ts).
 * It reads a sample definition off disk so the editor can render it without an
 * orchestrator. Disabled in production builds — it must never expose the
 * filesystem in a deployed editor.
 *
 *   GET /api/preview-sample?name=ai-router  ->  text/yaml
 */
export async function GET(req: Request) {
  if (process.env.NODE_ENV === "production") {
    return new NextResponse("not found", { status: 404 });
  }

  const name = new URL(req.url).searchParams.get("name") ?? "";
  // Allowlist slug-shaped names so the lookup can't escape the samples dir.
  if (!/^[a-z0-9][a-z0-9-]*$/.test(name)) {
    return NextResponse.json({ error: "invalid sample name" }, { status: 400 });
  }

  // `next dev` runs with cwd = editor/, so samples live one level up.
  const file = path.join(process.cwd(), "..", "samples", `${name}.yaml`);
  try {
    const yaml = await readFile(file, "utf8");
    return new NextResponse(yaml, {
      headers: { "content-type": "text/yaml; charset=utf-8" },
    });
  } catch {
    return NextResponse.json({ error: "sample not found" }, { status: 404 });
  }
}
