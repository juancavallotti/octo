import {
  BlockNode,
  EditorDocument,
  FlowDoc,
  SourceNode,
  isSlotField,
} from "./document";
import { duplicateNames, flowNames, referenceOptions } from "./identity";
import { getBlockSpec, getConnectorSpec, getSourceSpec } from "@/app/schema";
import type { FieldSpec } from "@/app/schema/types";

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
export interface ValidationResult {
  ok: boolean;
  issues: string[];
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
  label: string,
  issues: string[],
): void {
  for (const field of fields) {
    if (isSlotField(field)) continue; // nested sub-flows are checked separately
    const value = settings[field.name];

    if (field.ref) {
      const current = value === undefined || value === null ? "" : String(value);
      if (current === "") {
        if (field.required) issues.push(`${label}: ${field.label} is required.`);
      } else if (!referenceOptions(doc, field.ref).includes(current)) {
        issues.push(
          `${label}: ${field.label} references "${current}", which doesn't exist.`,
        );
      }
      continue;
    }

    if (field.required && isEmpty(value)) {
      issues.push(`${label}: ${field.label} is required.`);
    }
  }
}

function checkSource(
  source: SourceNode,
  doc: EditorDocument,
  label: string,
  issues: string[],
): void {
  if (!source.connector || !source.type) {
    issues.push(`${label}: source is incomplete (pick a connector and type).`);
    return;
  }
  const spec = getSourceSpec(source.connector, source.type);
  if (!spec) {
    issues.push(`${label}: unknown source "${source.connector}/${source.type}".`);
    return;
  }
  checkFields(spec.fields, source.settings, doc, `${label} source`, issues);
  // A flow source must bind a configured connector instance — the runtime fails
  // to build the flow otherwise ("source connector X is not configured").
  if (!source.connectorRef) {
    issues.push(
      `${label}: source needs a connection (bind a ${source.connector} connector).`,
    );
  } else if (
    !doc.connectors.some(
      (c) => c.name === source.connectorRef && c.type === source.connector,
    )
  ) {
    issues.push(
      `${label}: source connection "${source.connectorRef}" doesn't exist.`,
    );
  }
}

function checkBlock(
  block: BlockNode,
  doc: EditorDocument,
  path: string,
  issues: string[],
): void {
  const spec = getBlockSpec(block.type);
  const label = `${path} › ${block.name || spec?.label || block.type}`;
  if (!spec) {
    issues.push(`${path}: unknown block type "${block.type}".`);
    return;
  }
  checkFields(spec.fields, block.settings, doc, label, issues);

  for (const field of spec.fields) {
    if (!isSlotField(field)) continue;
    const subs = block.slots?.[field.name] ?? [];
    const filled = subs.filter((f) => f.process.length > 0);
    if (field.required && filled.length === 0) {
      issues.push(`${label}: ${field.label} needs at least one step.`);
    }
    subs.forEach((sub, i) => {
      if (sub.process.length === 0) return; // empty optional branch: nothing to check
      const suffix = subs.length > 1 ? ` #${i + 1}` : "";
      checkFlow(sub, doc, `${label} › ${field.label}${suffix}`, issues, false);
    });
  }
}

function checkFlow(
  flow: FlowDoc,
  doc: EditorDocument,
  label: string,
  issues: string[],
  isTopLevel: boolean,
): void {
  if (isTopLevel && !flow.source && flow.process.length === 0) {
    issues.push(`${label} is empty (add a source or a step).`);
  }
  if (flow.source) checkSource(flow.source, doc, label, issues);
  for (const block of flow.process) checkBlock(block, doc, label, issues);
}

/** Check the whole document; `ok` is true only when there are no issues. */
export function validateDocument(doc: EditorDocument): ValidationResult {
  const issues: string[] = [];

  if (doc.flows.length === 0) {
    issues.push("Add at least one flow to run.");
  }

  for (const name of duplicateNames(doc.connectors.map((c) => c.name))) {
    issues.push(`Connection name "${name}" is used more than once.`);
  }
  for (const name of duplicateNames(flowNames(doc))) {
    issues.push(`Flow name "${name}" is used more than once.`);
  }

  for (const conn of doc.connectors) {
    const label = `Connection "${conn.name || conn.type}"`;
    const spec = getConnectorSpec(conn.type);
    if (!spec) {
      issues.push(`${label}: unknown connector type "${conn.type}".`);
      continue;
    }
    checkFields(spec.settings, conn.settings, doc, label, issues);
  }

  doc.flows.forEach((flow, i) => {
    checkFlow(flow, doc, `Flow "${flow.name || `#${i + 1}`}"`, issues, true);
  });

  return { ok: issues.length === 0, issues };
}
