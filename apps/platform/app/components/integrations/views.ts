/**
 * The top-level views of the management page. Kept in a plain (non-"use client")
 * module so server components — e.g. the `/platform/integrations` route reading
 * `?view=` — import the real array, not a client-reference proxy.
 */
export const MANAGEMENT_VIEWS = ["integrations", "secrets"] as const;
export type ManagementView = (typeof MANAGEMENT_VIEWS)[number];
