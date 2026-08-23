/**
 * The error page's stylesheet, inlined into the document by renderErrorPage.
 *
 * Written out rather than imported from the app's Tailwind build because this page
 * must render with no stylesheet request in flight — see the note at the top of
 * errorPage.ts. The colors are the ones globals.css sets, and `color-scheme` plus
 * the media query give the same light/dark behaviour the rest of the platform has.
 */
export const PAGE_CSS = `
  :root {
    color-scheme: light dark;
    --bg: #ffffff;
    --fg: #171717;
    --muted: #71717a;
    --line: rgba(0, 0, 0, 0.1);
    --card: rgba(255, 255, 255, 0.8);
    --btn-bg: #18181b;
    --btn-fg: #ffffff;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #0a0a0a;
      --fg: #ededed;
      --muted: #a1a1aa;
      --line: rgba(255, 255, 255, 0.1);
      --card: rgba(24, 24, 27, 0.5);
      --btn-bg: #ffffff;
      --btn-fg: #18181b;
    }
  }
  * { box-sizing: border-box; }
  html, body { height: 100%; }
  body {
    margin: 0;
    background: var(--bg);
    color: var(--fg);
    font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto,
      "Helvetica Neue", Arial, sans-serif;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1.5rem;
    line-height: 1.5;
  }
  main {
    width: 100%;
    max-width: 30rem;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1.5rem;
    text-align: center;
  }
  /* The logo carries the only color on the page, so everything else stays neutral
     and lets it do that work. */
  .mark { width: 64px; height: auto; }
  .code {
    font-size: 3rem;
    font-weight: 600;
    letter-spacing: -0.025em;
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
  h1 { font-size: 1.125rem; font-weight: 500; margin: 0.25rem 0 0; }
  .body { color: var(--muted); font-size: 0.875rem; margin: 0.5rem 0 0; }
  .hint { color: var(--muted); font-size: 0.875rem; margin: 0.5rem 0 0; }
  .host {
    margin: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.375rem;
    width: 100%;
  }
  .host-label {
    font-size: 0.6875rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
  }
  .host code {
    display: block;
    max-width: 100%;
    overflow-wrap: anywhere;
    border: 1px solid var(--line);
    background: var(--card);
    border-radius: 0.5rem;
    padding: 0.5rem 0.75rem;
    font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
    font-size: 0.8125rem;
  }
  .action {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
    background: var(--btn-bg);
    color: var(--btn-fg);
    text-decoration: none;
    font-size: 0.875rem;
    font-weight: 500;
    border-radius: 0.5rem;
    padding: 0.5rem 1rem;
  }
  .action:hover { opacity: 0.85; }
`;
