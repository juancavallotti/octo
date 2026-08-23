"use client";

import { useState } from "react";

/**
 * The identity provider's mark, shown inside the sign-in button.
 *
 * A client component for one reason: the logo is best-effort. When it is not set
 * explicitly it defaults to the issuer's favicon (see oidc.config.ts), which is a
 * good guess but not a promise — plenty of issuers serve no favicon at all. If it
 * fails to load we drop it and leave the button reading "Sign in with {name}",
 * which a server component cannot do because only the browser learns of the 404.
 *
 * Plain `<img>` rather than `next/image` on purpose: the URL points at whatever
 * host the operator's provider lives on, and `next/image` would need that host
 * allow-listed in `next.config` per deployment.
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
