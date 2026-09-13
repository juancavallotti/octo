"use client";

import { EditorRoot, setCapabilities, type Capabilities } from "@octo/editor";
import PreviewLoader from "./PreviewLoader";

/**
 * Client wrapper for the dev-only `/preview` route: injects the capability schema
 * before the editor's first render, then renders a read-only editor whose canvas
 * PreviewLoader populates. A null schema leaves the empty bundled fallback in place.
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
