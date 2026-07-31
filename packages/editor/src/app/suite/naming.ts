/**
 * How a dolphin suite is named, and how its flow is read back.
 *
 * Both halves of this must be the same in every host or the Testing tab breaks in a way
 * that looks like a failed save: the file is written, but the tab asks for a flow the
 * store answers under a different name, so the suite reads as missing while it sits
 * right there. Standalone keeps suites on disk beside the flows; the platform keeps them
 * as orchestrator resources. Different storage, one rule.
 *
 * The naming follows the runtime's own: `orders_test.yaml` tests `orders`, the way
 * `orders_test.go` tests `orders.go`. `octo` skips these when it loads a directory as a
 * config (`IsTestFile` in runtime/core/runtime/config.go) and dolphin's discovery uses
 * the same suffix, which is what lets a suite live beside the flow it tests.
 */

/** The stem suffix that marks a test file, mirroring the runtime's TestFileSuffix. */
export const SUITE_SUFFIX = "_test";

/**
 * A suite filename: one safe segment ending `_test.yaml`/`_test.yml`. Must start with an
 * alphanumeric (so no dotfiles) and hold only word/dot/dash characters, which excludes
 * every path separator — so a name minted here can never reach a parent directory.
 */
const SUITE_FILE_RE = /^[A-Za-z0-9][A-Za-z0-9._-]*_test\.ya?ml$/i;

/**
 * The suite file's own `flow:` declaration. Anchored to column zero because that is
 * what makes it a top-level key: anything a YAML parser would read as nested, or as the
 * body of a block scalar, is indented further and cannot match. Quoted and bare forms,
 * with an optional trailing comment.
 */
const FLOW_DECL_RE = /^flow:[ \t]*(?:"([^"]*)"|'([^']*)'|([^#\n]*?))[ \t]*(?:#.*)?$/m;

/** Whether a filename is a dolphin suite rather than a flow definition. */
export function isSuiteFileName(name: string): boolean {
  return SUITE_FILE_RE.test(basename(name));
}

/**
 * The filename a new suite for `flow` is created as. Slugified, because a flow name is
 * free text and a filename is not — `Order Intake` becomes `order-intake_test.yaml`.
 * Every non-alphanumeric collapses to `-`, which also means no flow name can produce a
 * separator, a leading dot, or `..`.
 */
export function suiteFileName(flow: string): string {
  const slug =
    flow
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "flow";
  return `${slug}${SUITE_SUFFIX}.yaml`;
}

/**
 * Which flow a suite tests, read from the file's own declaration rather than from its
 * name — the direction that has to be right.
 *
 * suiteFileName is not reversible: `Order Intake` and `order-intake` both slug to
 * `order-intake`, and no amount of un-slugging recovers which one was meant. The suite
 * itself says, in the `flow:` key dolphin reads, so that is what we ask. The filename
 * stem is the fallback for a half-written file that has not declared one yet, so a suite
 * in progress still appears under a sensible name.
 */
export function flowOfSuite(content: string, fileName: string): string {
  const m = FLOW_DECL_RE.exec(content);
  const declared = (m?.[1] ?? m?.[2] ?? m?.[3] ?? "").trim();
  if (declared) return declared;
  return basename(fileName)
    .replace(/\.ya?ml$/i, "")
    .replace(/_test$/i, "");
}

/** The last segment of a path-like resource name (`.octo/tests/a_test.yaml` → `a_test.yaml`). */
function basename(name: string): string {
  return name.slice(name.lastIndexOf("/") + 1);
}
