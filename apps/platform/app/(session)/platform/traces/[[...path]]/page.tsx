/**
 * Marker page for the traces routes. The UI lives in the layout so it persists
 * across selection changes (see layout.tsx); this page exists only to make the
 * optional catch-all route — and every selection path under it, e.g.
 * /platform/traces/a/<deploymentId>/t/<traceId> — resolvable.
 */
export default function TracesPage() {
  return null;
}
