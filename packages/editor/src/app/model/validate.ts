import {
  BlockNode,
  EditorDocument,
  FlowDoc,
  SourceNode,
  isSlotField,
} from "./document";
import {
  duplicateNames,
  flowNames,
  isEnvPlaceholder,
  referenceOptions,
} from "./identity";
import { getBlockSpec, getConnectorSpec, getSourceSpec } from "../schema";
import type { FieldSpec } from "../schema/types";

/**
 * A lightweight pre-flight check used to gate the RUN button: it answers "would
 * the runtime even accept this document?" without trying to be the runtime. It
 * mirrors the failures the runner reports on load — empty/duplicate names, missing
 * required settings, dangling connector/flow references, empty required branches —
 * so the common breakages surface in the editor before we ever spawn `octo`.
 *
 * It is intentionally not exhaustive (CEL expressions, connector reachability,
 * etc. are left to the runner's own logs streamed into the panel).
 */

/**
 * One problem with the document. `message` is the human prose (the RUN button's
 * tooltip and the MCP `validate_definition` errors are just these strings), while
 * the ids say *where* it is, so the Problems tab can select the offending node
 * when the issue is clicked. Ids are absent for document-wide problems ("add at
 * least one flow") and for connections, which are not canvas nodes.
 */
export interface Issue {
  message: string;
  severity: "error" | "warning";
  /** The flow the problem is in, when it belongs to one. */
  flowId?: string;
  /** The offending block, when the problem is a block's. */
  blockId?: string;
}

export interface ValidationResult {
  ok: boolean;
  issues: Issue[];
}

/** The issue prose alone — for consumers that only render a list of strings. */
export function issueMessages(result: ValidationResult): string[] {
  return result.issues.map((i) => i.message);
}

/**
 * Where the checker currently is: the label prefix built up as it descends (the
 * prose users read) plus the ids of the nodes it is inside, so an issue raised
 * deep in a composite still points at something selectable on the canvas.
 */
interface Scope {
  label: string;
  flowId?: string;
  blockId?: string;
}

/** Raise an issue at `scope`, prefixing its label. */
function raise(issues: Issue[], scope: Scope, message: string): void {
  issues.push({
    message: `${scope.label}: ${message}`,
    severity: "error",
    flowId: scope.flowId,
    blockId: scope.blockId,
  });
}

/** Whether a scalar setting value should count as "not provided". */
function isEmpty(value: unknown): boolean {
  if (value === undefined || value === null) return true;
  if (typeof value === "string") return value.trim() === "";
  if (Array.isArray(value)) return value.length === 0;
  if (typeof value === "object") return Object.keys(value).length === 0;
  return false; // numbers and booleans are always "present"
}

/** Validate the scalar (non-slot) fields of a block/source/connector settings. */
function checkFields(
  fields: FieldSpec[],
  settings: Record<string, unknown>,
  doc: EditorDocument,
  scope: Scope,
  issues: Issue[],
): void {
  for (const field of fields) {
    if (isSlotField(field)) continue; // nested sub-flows are checked separately
    const value = settings[field.name];

    if (field.ref) {
      const current = value === undefined || value === null ? "" : String(value);
      if (current === "") {
        if (field.required) raise(issues, scope, `${field.label} is required.`);
      } else if (!referenceOptions(doc, field.ref).includes(current)) {
        raise(
          issues,
          scope,
          `${field.label} references "${current}", which doesn't exist.`,
        );
      }
      continue;
    }

    if (field.required && isEmpty(value)) {
      raise(issues, scope, `${field.label} is required.`);
    }
  }
}

function checkSource(
  source: SourceNode,
  doc: EditorDocument,
  scope: Scope,
  issues: Issue[],
): void {
  if (!source.connector || !source.type) {
    raise(issues, scope, "source is incomplete (pick a connector and type).");
    return;
  }
  const spec = getSourceSpec(source.connector, source.type);
  if (!spec) {
    raise(issues, scope, `unknown source "${source.connector}/${source.type}".`);
    return;
  }
  checkFields(spec.fields, source.settings, doc, { ...scope, label: `${scope.label} source` }, issues);
  // The connector binding is optional: a lone connection of the matching type
  // binds implicitly (serialize resolves it). It only has to be chosen when the
  // choice is ambiguous — two or more connections of that type. An explicit
  // binding, however, must still resolve.
  // Two different questions, so two different sets.
  //
  // An explicit binding only has to be *plausible*, and a connection whose type is an
  // environment placeholder is: the editor cannot know what it resolves to, so
  // refusing it would be a claim it cannot support.
  //
  // Ambiguity is the opposite. It asks whether the implicit binding is unclear, and
  // an implicit binding resolves against connections of the matching type — so a
  // placeholder that might turn out to be something else entirely must not count.
  // Otherwise one http connection alongside one unrelated placeholder would demand a
  // choice between them that the author does not have to make.
  const declared = doc.connectors.filter((c) => c.type === source.connector);
  if (source.connectorRef) {
    const bindable = doc.connectors.filter(
      (c) => c.type === source.connector || isEnvPlaceholder(c.type),
    );
    if (!bindable.some((c) => c.name === source.connectorRef)) {
      raise(
        issues,
        scope,
        `source connection "${source.connectorRef}" doesn't exist.`,
      );
    }
  } else if (declared.length >= 2) {
    raise(
      issues,
      scope,
      `multiple ${source.connector} connections exist — choose one for the source.`,
    );
  }
}

function checkBlock(
  block: BlockNode,
  doc: EditorDocument,
  scope: Scope,
  issues: Issue[],
): void {
  const spec = getBlockSpec(block.type);
  // From here on the issues belong to this block, so they carry its id.
  const here: Scope = {
    label: `${scope.label} › ${block.name || spec?.label || block.type}`,
    flowId: scope.flowId,
    blockId: block.id,
  };
  if (!spec) {
    raise(issues, { ...scope, blockId: block.id }, `unknown block type "${block.type}".`);
    return;
  }
  checkFields(spec.fields, block.settings, doc, here, issues);

  for (const field of spec.fields) {
    if (!isSlotField(field)) continue;
    const subs = block.slots?.[field.name] ?? [];
    const filled = subs.filter((f) => f.process.length > 0);
    if (field.required && filled.length === 0) {
      raise(issues, here, `${field.label} needs at least one step.`);
    }
    subs.forEach((sub, i) => {
      if (sub.process.length === 0) return; // empty optional branch: nothing to check
      const suffix = subs.length > 1 ? ` #${i + 1}` : "";
      // A sub-flow's own issues belong to the sub-flow, not to the composite that
      // holds it — selecting the composite would hide the block actually at fault.
      checkFlow(
        sub,
        doc,
        { label: `${here.label} › ${field.label}${suffix}`, flowId: sub.id },
        issues,
        false,
      );
    });
  }
}

function checkFlow(
  flow: FlowDoc,
  doc: EditorDocument,
  scope: Scope,
  issues: Issue[],
  isTopLevel: boolean,
): void {
  if (isTopLevel && !flow.source && flow.process.length === 0) {
    issues.push({
      message: `${scope.label} is empty (add a source or a step).`,
      severity: "error",
      flowId: scope.flowId,
    });
  }
  if (flow.source) checkSource(flow.source, doc, scope, issues);
  for (const block of flow.process) checkBlock(block, doc, scope, issues);
}

/** Check the whole document; `ok` is true only when there are no issues. */
export function validateDocument(doc: EditorDocument): ValidationResult {
  const issues: Issue[] = [];

  if (doc.flows.length === 0) {
    issues.push({ message: "Add at least one flow to run.", severity: "error" });
  }

  for (const name of duplicateNames(doc.connectors.map((c) => c.name))) {
    issues.push({
      message: `Connection name "${name}" is used more than once.`,
      severity: "error",
    });
  }
  for (const name of duplicateNames(flowNames(doc))) {
    issues.push({
      message: `Flow name "${name}" is used more than once.`,
      severity: "error",
    });
  }

  for (const conn of doc.connectors) {
    // A connection is not a canvas node, so its issues carry no ids — clicking one
    // has nothing to select.
    const scope: Scope = { label: `Connection "${conn.name || conn.type}"` };
    // A type that is a bare ${NAME} is resolved from the environment at load, so
    // there is nothing here to look up and nothing meaningful to say about the
    // settings either — which fields are valid depends on which type it becomes.
    // Reporting it as unknown would block Run and Deploy on a config the runtime
    // loads perfectly well.
    if (isEnvPlaceholder(conn.type)) {
      continue;
    }
    const spec = getConnectorSpec(conn.type);
    if (!spec) {
      raise(issues, scope, `unknown connector type "${conn.type}".`);
      continue;
    }
    checkFields(spec.settings, conn.settings, doc, scope, issues);
  }

  doc.flows.forEach((flow, i) => {
    checkFlow(
      flow,
      doc,
      { label: `Flow "${flow.name || `#${i + 1}`}"`, flowId: flow.id },
      issues,
      true,
    );
  });

  return { ok: issues.length === 0, issues };
}
