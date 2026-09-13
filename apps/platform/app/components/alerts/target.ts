import type { AlertCondition, AlertScope } from "@/app/model/alerts";

/**
 * What a watch is about.
 *
 * The service scopes each condition on its own, but that is not the question
 * anybody starts from: somebody opening this page has an app in mind, so the
 * editor asks once, at the top, and writes the answer onto every condition.
 *
 * The axes differ by source because the tables do:
 *
 *   traces     integrationId — survives a rollout, which a deployment id does not
 *   logs       appName       — the logs table has no integration column
 *   pod stats  deploymentId  — the Redis keys are per deployment; nothing else exists
 *
 * So "one app" is three different predicates underneath.
 */
export interface WatchTarget {
  integrationId: string;
  deploymentId: string;
  appName: string;
}

export const NO_TARGET: WatchTarget = {
  integrationId: "",
  deploymentId: "",
  appName: "",
};

export function hasTarget(target: WatchTarget): boolean {
  return (
    target.integrationId !== "" ||
    target.deploymentId !== "" ||
    target.appName !== ""
  );
}

/** The scope one source needs to address this target. */
export function scopeFor(
  target: WatchTarget,
  source: AlertCondition["source"],
): Pick<AlertScope, "integrationId" | "deploymentId" | "appName"> {
  switch (source) {
    case "traces":
      // Prefer the integration: a rollout keeps it and changes the deployment,
      // and a watch that stopped matching after a deploy would be the worst kind
      // of silence.
      return target.integrationId
        ? { integrationId: target.integrationId }
        : { deploymentId: target.deploymentId };
    case "logs":
      return target.appName
        ? { appName: target.appName }
        : { deploymentId: target.deploymentId };
    default:
      return { deploymentId: target.deploymentId };
  }
}

/**
 * Write the target onto every condition, leaving the source-specific refinements
 * — log levels, a message search, a pod list — alone.
 *
 * A pod-stat condition keeps a deployment somebody set by hand: pod stats are the
 * one source where the target's deployment may genuinely be the wrong one, since
 * an app can have several and the metrics live under a particular one.
 */
export function applyTarget(
  conditions: AlertCondition[],
  target: WatchTarget,
): AlertCondition[] {
  return conditions.map((condition) => {
    const keep = condition.scope ?? {};
    const pinned =
      condition.source === "pod_stats" && keep.deploymentId
        ? { deploymentId: keep.deploymentId }
        : {};
    return {
      ...condition,
      scope: {
        ...stripTarget(keep),
        ...scopeFor(target, condition.source),
        ...pinned,
      },
    };
  });
}

/**
 * Fill in the target on any condition that has none — a condition just added, or
 * one whose measure changed, which clears the source-specific scope. Either would
 * otherwise be measured over the whole installation while the form says it is
 * about one app.
 *
 * Only where it is missing. A condition somebody deliberately scoped elsewhere
 * keeps it, which is what stops every keystroke normalising a definition written
 * over the API.
 */
export function fillTarget(
  conditions: AlertCondition[],
  target: WatchTarget,
): AlertCondition[] {
  if (!hasTarget(target)) return conditions;
  return conditions.map((condition) => {
    const scope = condition.scope ?? {};
    const scoped = scope.integrationId || scope.deploymentId || scope.appName;
    if (scoped) return condition;
    return {
      ...condition,
      scope: { ...scope, ...scopeFor(target, condition.source) },
    };
  });
}

/**
 * Read the target back off a stored watch.
 *
 * Best effort by design. A watch written over the API may scope its conditions
 * differently from one another, and no target describes that honestly — so this
 * reports what it can find rather than picking one and rewriting the rest.
 */
export function targetOf(conditions: AlertCondition[]): WatchTarget {
  const target = { ...NO_TARGET };
  for (const condition of conditions) {
    const scope = condition.scope ?? {};
    target.integrationId ||= scope.integrationId ?? "";
    target.deploymentId ||= scope.deploymentId ?? "";
    target.appName ||= scope.appName ?? "";
  }
  return target;
}

/**
 * Whether the conditions all point at the same thing.
 *
 * When they do not, applying a target would rewrite somebody's deliberate
 * arrangement, so the editor warns instead of silently normalising it.
 */
export function targetsAgree(conditions: AlertCondition[]): boolean {
  // Compared field by field, and only where a field is actually set: scopeFor
  // writes a different field per source, so two conditions disagree only when
  // both name the same axis and name it differently.
  const axes = ["integrationId", "deploymentId", "appName"] as const;
  return axes.every((axis) => {
    const values = new Set(
      conditions
        .map((c) => (c.scope ?? {})[axis])
        .filter((v): v is string => Boolean(v)),
    );
    return values.size <= 1;
  });
}

/** Everything a scope holds that is not the target. */
function stripTarget(scope: AlertScope): AlertScope {
  const { integrationId, deploymentId, appName, ...rest } = scope;
  void integrationId;
  void deploymentId;
  void appName;
  return rest;
}
