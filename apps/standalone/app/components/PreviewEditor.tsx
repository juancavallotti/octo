"use client";

import { EditorRoot, setCapabilities, type Capabilities } from "@octo/editor";
import PreviewLoader from "./PreviewLoader";

/**
 * Client wrapper for the dev-only `/preview` route: injects the runtime-generated
 * capability schema before the editor's first render, then renders a read-only
 * editor whose canvas is populated by PreviewLoader. Without this the palette
 * would fall back to the empty bundled `capabilities.json` — which is why the
 * Playwright screenshot harness used to shoot an empty palette. Mirrors
 * StandaloneEditor's `setCapabilities` call; a null value leaves the fallback in
 * place.
 */
export default function PreviewEditor({
  sample,
  capabilities,
}: {
  sample?: string;
  capabilities?: Capabilities | null;
}) {
  setCapabilities(capabilities);
  return <EditorRoot loader={<PreviewLoader sample={sample} />} />;
}
