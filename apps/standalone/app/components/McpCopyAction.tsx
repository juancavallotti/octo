"use client";

import { useEffect, useState } from "react";
import { CopyMcpUrlButton } from "@octo/editor";
import { desktopBridge } from "../desktop";

/**
 * Where this app's MCP endpoint is, for the header's copy button. A hosting shell is
 * asked first, since it owns the address it started the server on; otherwise the
 * endpoint is this origin's `/mcp`. Resolved in an effect, because `window` does not
 * exist on the server and the bridge answers asynchronously.
 */
export default function McpCopyAction() {
  const [url, setUrl] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const bridge = desktopBridge();
    const resolve = async () => {
      const fromShell = bridge ? await bridge.mcpUrl().catch(() => null) : null;
      if (!cancelled) setUrl(fromShell ?? `${window.location.origin}/mcp`);
    };
    void resolve();
    return () => {
      cancelled = true;
    };
  }, []);

  if (!url) return null;
  return <CopyMcpUrlButton url={url} />;
}
