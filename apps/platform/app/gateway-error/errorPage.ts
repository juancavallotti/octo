/**
 * The HTML the gateway serves when a request never reaches an integration.
 *
 * A hand-written string rather than a React page, deliberately. This renders when
 * something is already broken — a hostname that resolves to nothing, a deployment
 * that is down — and it is served to whoever typed the URL, not to a signed-in
 * operator. So it carries no JavaScript, no font or image request, and no CSS
 * file: one response, no dependencies, nothing else that can fail behind it. It is
 * also why the logo is a data URI rather than `/octo-logo.png` — a second request
 * from this page would be routed straight back here and come back as HTML, so a
 * linked logo would render as a broken image.
 *
 * It follows the platform's own not-found page in voice and proportion, and its
 * palette is the same one `globals.css` sets, written out because there is no
 * stylesheet in the loop. The mark is the real logo, inlined as a data URI for the
 * same reason — see logo.ts.
 */

/** What each status means to the person who hit it, and what they can do next. */
interface Explanation {
  /** The large word under the code. */
  title: string;
  /** One or two sentences naming the likely cause. */
  body: string;
  /** Whether waiting is a reasonable thing to suggest. */
  transient: boolean;
}

const EXPLANATIONS: Record<number, Explanation> = {
  404: {
    title: "Nothing is deployed here",
    body:
      "No integration is serving this address. It may never have been deployed, it may have been undeployed since you got this link, or the subdomain may be misspelled.",
    transient: false,
  },
  502: {
    title: "This integration is not answering",
    body:
      "The deployment exists, but nothing behind it accepted the request. It may be restarting, or it may be failing to start.",
    transient: true,
  },
  503: {
    title: "This integration is not ready yet",
    body:
      "The deployment exists and is still coming up, or it is draining after a rollout. Requests are refused until it reports itself ready.",
    transient: true,
  },
  504: {
    title: "This integration took too long",
    body:
      "The deployment accepted the request but did not answer in time. A long-running flow behind a gateway timeout usually belongs on a queue instead.",
    transient: true,
  },
};

/** Anything not named above gets a truthful generic. */
const FALLBACK: Explanation = {
  title: "This request could not be served",
  body: "The gateway could not route this request to an integration.",
  transient: false,
};

export function explanationFor(status: number): Explanation {
  return EXPLANATIONS[status] ?? FALLBACK;
}

/**
 * escapeHTML makes a value safe to interpolate into the document.
 *
 * The hostname is attacker-controlled — anyone can send any Host header — so it is
 * escaped, and capped, before it goes anywhere near the page. Showing it is worth
 * this: a typo is invisible in prose and obvious when the address is on screen.
 */
export function escapeHTML(value: string): string {
  return value.replace(
    /[&<>"']/g,
    (c) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[c] as string,
  );
}

/** Hostnames longer than this are truncated; a real one never comes close. */
const MAX_HOST = 120;

/** Only characters a hostname may contain, so nothing else reaches the page. */
const HOST_SHAPE = /^[a-zA-Z0-9.\-:_[\]]+$/;

/**
 * cleanHost returns a hostname safe and sensible to display, or "" when there is
 * nothing worth showing. A value that is not shaped like a hostname is dropped
 * rather than escaped and displayed: it did not come from a browser addressing a
 * deployment, so showing it back only helps whoever sent it.
 */
export function cleanHost(raw: string | null): string {
  if (!raw) return "";
  const first = raw.split(",")[0].trim();
  if (!first || first.length > MAX_HOST) return "";
  return HOST_SHAPE.test(first) ? first : "";
}

/** Options the route resolves from the request before rendering. */
export interface ErrorPageOptions {
  status: number;
  /** The hostname that was asked for, already validated. Empty hides the line. */
  host: string;
  /** Absolute URL of the platform, for the way-out link. Empty hides the button. */
  platformURL: string;
}

import { LOGO_DATA_URI } from "./logo";
import { PAGE_CSS } from "./styles";

export function renderErrorPage({
  status,
  host,
  platformURL,
}: ErrorPageOptions): string {
  const { title, body, transient } = explanationFor(status);
  const safeHost = escapeHTML(host);
  const safeTitle = escapeHTML(title);

  const hostLine = safeHost
    ? `<p class="host"><span class="host-label">Requested</span><code>${safeHost}</code></p>`
    : "";

  const hint = transient
    ? `<p class="hint">If this deployment was just rolled out, try again in a moment.</p>`
    : "";

  // Only ever an absolute https/http URL the chart supplied, never anything from
  // the request — a link built from a Host header would let a stranger choose
  // where this page points.
  const action = platformURL
    ? `<a class="action" href="${escapeHTML(platformURL)}">Go to the Octo platform</a>`
    : "";

  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>${status} — ${safeTitle}</title>
<style>${PAGE_CSS}</style>
</head>
<body>
<main>
  <img class="mark" src="${LOGO_DATA_URI}" alt="Octo" width="64" height="65">
  <div>
    <p class="code">${status}</p>
    <h1>${safeTitle}</h1>
    <p class="body">${escapeHTML(body)}</p>
    ${hint}
  </div>
  ${hostLine}
  ${action}
</main>
</body>
</html>
`;
}
