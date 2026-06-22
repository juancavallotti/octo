import StandaloneEditor from "./components/StandaloneEditor";

/**
 * `/` opens a fresh flow; `/?file=<id>` opens a saved one (the editor loads it
 * from the local-disk filesystem). The id round-trips through the query so a
 * reload reopens the same flow.
 */
export default async function Home({
  searchParams,
}: {
  searchParams: Promise<{ file?: string }>;
}) {
  const { file } = await searchParams;
  return <StandaloneEditor file={file} />;
}
