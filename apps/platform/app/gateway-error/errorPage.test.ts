import { describe, expect, it } from "vitest";
import { cleanHost, escapeHTML, explanationFor, renderErrorPage } from "./errorPage";

describe("cleanHost", () => {
  it("keeps a real hostname", () => {
    expect(cleanHost("notathing.apps.octopaas.dev")).toBe(
      "notathing.apps.octopaas.dev",
    );
  });

  it("takes the first entry of a forwarded chain", () => {
    expect(cleanHost("a.apps.example.com, proxy.internal")).toBe(
      "a.apps.example.com",
    );
  });

  // The Host header is chosen by whoever sent the request, so anything that is not
  // shaped like a hostname is dropped rather than shown back. Escaping alone would
  // be enough to be safe; dropping is what stops the page becoming a billboard.
  const rejected: [string | null, string][] = [
    ["<script>alert(1)</script>", "markup"],
    ["evil.com/<img src=x onerror=1>", "a path with markup"],
    ["visit spam.example.com now", "prose"],
    ["a".repeat(200), "an absurdly long value"],
    ["", "an empty value"],
    [null, "a missing header"],
  ];
  it.each(rejected)("drops %s (%s)", (input) => {
    expect(cleanHost(input)).toBe("");
  });

  it("allows the characters a hostname really has", () => {
    expect(cleanHost("my-app.apps.example.com:8443")).toBe(
      "my-app.apps.example.com:8443",
    );
  });
});

describe("escapeHTML", () => {
  it("neutralizes every character that could break out of the document", () => {
    expect(escapeHTML(`<>&"'`)).toBe("&lt;&gt;&amp;&quot;&#39;");
  });
});

describe("explanationFor", () => {
  it("names the cause for each status the gateway produces", () => {
    expect(explanationFor(404).title).toMatch(/deployed/i);
    expect(explanationFor(502).transient).toBe(true);
    expect(explanationFor(503).transient).toBe(true);
  });

  it("falls back to something truthful for anything else", () => {
    expect(explanationFor(418).title).toBe(FALLBACK_TITLE);
  });
});

const FALLBACK_TITLE = "This request could not be served";

describe("renderErrorPage", () => {
  const page = (over: Partial<Parameters<typeof renderErrorPage>[0]> = {}) =>
    renderErrorPage({
      status: 404,
      host: "notathing.apps.octopaas.dev",
      platformURL: "https://octo.example.com",
      ...over,
    });

  it("carries the real logo rather than an invented mark", () => {
    expect(page()).toContain('alt="Octo"');
  });

  it("is self-contained: no script, and nothing fetched from anywhere", () => {
    const html = page();
    expect(html).not.toMatch(/<script/i);
    // No stylesheet, image, font or any other subresource: this renders when
    // things are already broken, and a second request would be routed back here
    // and come back as HTML.
    expect(html).not.toMatch(/<link\b/i);
    expect(html).not.toMatch(/url\(/i);
    expect(html).not.toMatch(/@import/i);
    // The logo is the one src on the page, and it is a data URI — a src that
    // fetches nothing. Anything else would be a request, which is the thing this
    // test exists to forbid.
    const sources = html.match(/\bsrc="([^"]*)"/g) ?? [];
    expect(sources).toHaveLength(1);
    expect(sources[0]).toMatch(/^src="data:image\/png;base64,/);
    // The only URL on the page is the way-out link the chart supplied.
    const urls = html.match(/https?:\/\/[^"'\s)]+/g) ?? [];
    expect(urls).toEqual(["https://octo.example.com"]);
  });

  it("shows the status and the hostname that was asked for", () => {
    const html = page();
    expect(html).toContain("404");
    expect(html).toContain("notathing.apps.octopaas.dev");
  });

  it("escapes the hostname rather than interpolating it raw", () => {
    // cleanHost drops this upstream; the renderer must not depend on that.
    const html = page({ host: `</code><script>alert(1)</script>` });
    expect(html).not.toContain("<script>alert(1)</script>");
    expect(html).toContain("&lt;script&gt;");
  });

  it("omits the hostname line when there is nothing worth showing", () => {
    expect(page({ host: "" })).not.toContain("Requested");
  });

  it("omits the way-out link when no platform URL is configured", () => {
    expect(page({ platformURL: "" })).not.toContain("class=\"action\"");
  });

  it("suggests retrying only for the transient statuses", () => {
    expect(page({ status: 503 })).toMatch(/try again/i);
    expect(page({ status: 404 })).not.toMatch(/try again/i);
  });

  it("tells crawlers not to index it", () => {
    expect(page()).toContain('name="robots" content="noindex"');
  });
});
