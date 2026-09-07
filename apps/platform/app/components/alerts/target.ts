import type { AlertCondition, AlertScope } from "@/app/model/alerts";

/**
 * What a watch is about.
 *
 * The service scopes each condition on its own, which is the right shape for it
 * to store: a watch genuinely may ask about two different things. But it is not
 * the question anybody starts from. Somebody opening this page has an app in mind
 * and wants to say something about it, so the editor asks once, at the top, and
 * writes the answer onto every condition.
 *
 * The axes differ by source because the tables do:
 *
 *   traces     integrationId — survives a rollout, which a deployment id does not
 *   logs       appName       — the logs table has no integration column
 *   pod stats  deploymentId  — the Redis keys are per deployment; nothing else exists
 *
 * So "one app" is three different predicates underneath, which is exactly the
 * kind of thing a form should not make somebody assemble by hand.
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
 * Fill in the target on any condition that has none.
 *
 * Two things produce one: adding a condition, and changing a condition's
 * measure — which clears the source-specific scope, because log levels mean
 * nothing to a pod-stat metric. Both would otherwise leave a condition measured
 * over the whole installation while the form says it is about one app, which is
 * a watch that fires for somebody else's traffic.
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
 * differently from one another, and there is no target that describes that
 * honestly — so this reports what it can find and the editor says when the
 * conditions disagree, rather than picking one and quietly rewriting the rest.
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
  const seen = new Set(
    conditions.map((c) => {
      const scope = c.scope ?? {};
      return [
        scope.integrationId ?? "",
        scope.deploymentId ?? "",
        scope.appName ?? "",
      ].join("|");
    }),
  );
  return seen.size <= 1;
}

/** Everything a scope holds that is not the target. */
function stripTarget(scope: AlertScope): AlertScope {
  const { integrationId, deploymentId, appName, ...rest } = scope;
  void integrationId;
  void deploymentId;
  void appName;
  return rest;
}
