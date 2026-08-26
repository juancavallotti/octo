/**
 * Client-side helpers for the file manager's import/export: turning what the
 * platform holds into a file the browser saves, and reading a picked file back.
 *
 * Two shapes travel: a single YAML definition — an integration's `definition` is
 * already the runtime YAML, so exporting one is just offering that string as a
 * file — and a **bundle**, the zip the orchestrator builds from an integration
 * and every resource it owns. Everything here is naming and Blob plumbing; what
 * goes *in* a bundle is the orchestrator's business.
 */

/** The content type the orchestrator serves and accepts a bundle as. */
const ARCHIVE_TYPE = "application/zip";

/** Slugify a display name into a safe filename stem (falls back to "integration").
 * Mirrors the orchestrator's own rule, so a bundle downloaded from the UI is
 * named the same as one fetched from the API. */
export function slug(name: string): string {
  const s = name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return s || "integration";
}

/**
 * Offer `blob` to the browser as a download named `filename`. Builds an object
 * URL and clicks a transient anchor, then revokes it — the one piece of DOM
 * plumbing every export here goes through.
 */
export function downloadBlob(filename: string, blob: Blob): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/** Trigger a browser download of an integration's definition as `<slug>.yaml`. */
export function downloadDefinition(name: string, definition: string): void {
  downloadBlob(
    `${slug(name)}.yaml`,
    new Blob([definition], { type: "application/yaml" }),
  );
}

/**
 * Trigger a browser download of a bundle archive. `tag` is the version it was
 * exported from, which the filename carries so two exports of the same
 * integration at different versions do not land on top of each other.
 */
export function downloadBundle(
  name: string,
  archive: Uint8Array,
  tag?: string | null,
): void {
  const stem = tag ? `${slug(name)}-${slug(tag)}` : slug(name);
  downloadBlob(
    `${stem}.zip`,
    new Blob([archive as BlobPart], { type: ARCHIVE_TYPE }),
  );
}

/**
 * Trigger a browser download of one resource, under its own name. A resource
 * name is path-like, so only the last segment can be a filename — the directories
 * a bundle preserves have nowhere to go in a single-file download.
 */
export function downloadResource(name: string, content: BlobPart): void {
  const base = name.split("/").pop() || name;
  downloadBlob(base, new Blob([content], { type: "text/plain" }));
}

/** Derive an integration name from an uploaded filename (strip path and the
 * .yaml/.yml/.zip extension). */
export function nameFromFilename(filename: string): string {
  const base = filename.split(/[\\/]/).pop() ?? filename;
  const stem = base.replace(/\.(ya?ml|zip)$/i, "").trim();
  return stem || "Imported integration";
}

/** Whether a picked file is a bundle archive rather than a bare definition. The
 * name decides: browsers report zip under several types (and sometimes none at
 * all), while the extension is what the user actually chose. */
export function isBundleFile(file: File): boolean {
  return /\.zip$/i.test(file.name);
}

/** Read a picked file as bytes, for upload through the model layer. */
export async function readFileBytes(file: File): Promise<Uint8Array> {
  return new Uint8Array(await file.arrayBuffer());
}
