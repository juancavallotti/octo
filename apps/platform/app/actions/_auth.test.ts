import { describe, expect, it, vi } from "vitest";

const session = { user: { id: "user-1" } };
vi.mock("@/app/auth/guard", () => ({
  AuthError: class extends Error {},
  ForbiddenError: class extends Error {},
  requireRole: async () => session,
  requireSession: async () => session,
  writeRoles: [],
}));

import { callerEnvironment, runWithEnvironment } from "@/app/auth/callerEnvironment";
import { withAdmin, withAdminUser, withRead } from "./_auth";

/** What environment an action sees, run from a caller who has picked one. */
function seenFrom(environment: string, gate: typeof withRead) {
  return runWithEnvironment(environment, () =>
    gate(async () => ({ ok: true as const, data: await callerEnvironment() })),
  );
}

describe("the admin gates", () => {
  it("act in the default environment whichever one the caller picked", async () => {
    expect(await seenFrom("stage", withAdmin)).toEqual({ ok: true, data: "" });
    const res = await runWithEnvironment("stage", () =>
      withAdminUser(async () => ({ ok: true as const, data: await callerEnvironment() })),
    );
    expect(res).toEqual({ ok: true, data: "" });
  });

  it("leave every other gate in the caller's environment", async () => {
    expect(await seenFrom("stage", withRead)).toEqual({ ok: true, data: "stage" });
  });
});
