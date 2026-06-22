import { handlers } from "@/auth";

export const runtime = "nodejs";

/** Auth.js sign-in / callback / sign-out / session endpoints under /api/auth/*. */
export const { GET, POST } = handlers;
