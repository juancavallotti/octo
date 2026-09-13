"use client";

import { useState } from "react";

/**
 * The identity provider's mark, shown inside the sign-in button.
 *
 * A client component because the logo is best-effort: unset, it defaults to the
 * issuer's favicon, which plenty of issuers do not serve. Only the browser learns
 * of the 404, and the button is left reading "Sign in with {name}".
 *
 * Plain `<img>` rather than `next/image`: the URL points at whatever host the
 * operator's provider lives on, which `next/image` would need allow-listed in
 * `next.config` per deployment.
 */
export default function ProviderLogo({ src, alt }: { src: string; alt: string }) {
  const [failed, setFailed] = useState(false);
  if (failed) return null;
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={src}
      alt={alt}
      width={16}
      height={16}
      className="h-4 w-4 rounded-sm object-contain"
      onError={() => setFailed(true)}
    />
  );
}
