import { notFound } from "next/navigation";
import EditorShell from "@/app/components/EditorShell";
import PreviewLoader from "@/app/components/PreviewLoader";

/**
 * Dev-only preview route: `/preview?sample=<name>` renders a repo sample on the
 * editor canvas (loaded client-side by PreviewLoader from /api/preview-sample),
 * with no orchestrator. Used by the Playwright screenshot harness to capture how
 * flows look. Returns 404 in production builds.
 */
export default async function PreviewPage({
  searchParams,
}: {
  searchParams: Promise<{ sample?: string }>;
}) {
  if (process.env.NODE_ENV === "production") notFound();
  const { sample } = await searchParams;
  return <EditorShell loader={<PreviewLoader sample={sample} />} />;
}
