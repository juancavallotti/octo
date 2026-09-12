"use server";

/**
 * Server actions for the site's data-retention policy. Authorizes and delegates
 * to the aggregator client (`_retention.ts`); the model unwraps the ActionResult.
 *
 * Administrators only, the read included: a retention policy says how long this
 * installation keeps anything, which is not a general-audience fact. Running a
 * sweep is the most consequential write in the admin section, because what it
 * deletes does not come back.
 */

import type {
  RetentionPolicy,
  RetentionPolicyInput,
  RetentionRun,
} from "@/app/model/retention";
import { withAdmin } from "./_auth";
import * as retention from "./_retention";
import type { ActionResult } from "./_client";

export async function getRetention(): Promise<ActionResult<RetentionPolicy>> {
  return withAdmin(() => retention.getRetention());
}

export async function saveRetention(
  input: RetentionPolicyInput,
): Promise<ActionResult<RetentionPolicy>> {
  return withAdmin(() => retention.saveRetention(input));
}

export async function runRetention(): Promise<ActionResult<RetentionRun>> {
  return withAdmin(() => retention.runRetention());
}
