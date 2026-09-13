import { NextResponse } from "next/server";
import { readFile } from "node:fs/promises";
import path from "node:path";

/**
 * Dev-only loader for the repo's `samples/*.yaml` flows, serving a sample definition
 * off disk for the `/preview` route. Disabled in production builds: it must never
 * expose the filesystem in a deployed editor.
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

  // `next dev` runs with cwd = apps/standalone/, so the repo's samples/ dir is
  // two levels up.
  const file = path.join(process.cwd(), "..", "..", "samples", `${name}.yaml`);
  try {
    const yaml = await readFile(file, "utf8");
    return new NextResponse(yaml, {
      headers: { "content-type": "text/yaml; charset=utf-8" },
    });
  } catch {
    return NextResponse.json({ error: "sample not found" }, { status: 404 });
  }
}
