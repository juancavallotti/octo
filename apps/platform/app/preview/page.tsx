import { notFound } from "next/navigation";
import EditorRoot from "@/app/components/EditorRoot";
import PreviewLoader from "@/app/components/PreviewLoader";

/**
 * Dev-only preview route: `/preview?sample=<name>` renders a repo sample on the
 * editor canvas (loaded client-side by PreviewLoader from /api/preview-sample),
 * with neither a filesystem nor a run capability — so it's a pure read-only
 * editor (no Save, no RUN). Used by the Playwright screenshot harness to capture
 * how flows look. Returns 404 in production builds.
 */
export default async function PreviewPage({
  searchParams,
}: {
  searchParams: Promise<{ sample?: string }>;
}) {
  if (process.env.NODE_ENV === "production") notFound();
  const { sample } = await searchParams;
  return <EditorRoot loader={<PreviewLoader sample={sample} />} />;
}
