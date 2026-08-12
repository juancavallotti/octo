"use server";

/**
 * Server actions for the site's data-retention policy. Authorizes and delegates
 * to the aggregator client (`_retention.ts`); the model unwraps the ActionResult.
 *
 * Reading the policy needs a session. Both of the others are writes behind
 * `withWrite` — and running a sweep is the most consequential write in the admin
 * section, because what it deletes does not come back.
 */

import type {
  RetentionPolicy,
  RetentionPolicyInput,
  RetentionRun,
} from "@/app/model/retention";
import { withRead, withWrite } from "./_auth";
import * as retention from "./_retention";
import type { ActionResult } from "./_client";

export async function getRetention(): Promise<ActionResult<RetentionPolicy>> {
  return withRead(() => retention.getRetention());
}

export async function saveRetention(
  input: RetentionPolicyInput,
): Promise<ActionResult<RetentionPolicy>> {
  return withWrite(() => retention.saveRetention(input));
}

export async function runRetention(): Promise<ActionResult<RetentionRun>> {
  return withWrite(() => retention.runRetention());
}
