# Octo Editor — Coding Standards

Conventions for the Next.js code under `apps/` (the **Octo** visual editor and its
hosts, `apps/platform` and `apps/standalone`) and the shared `packages/`. These
complement the Go [coding-standards.md](coding-standards.md). Keep this in sync
with each app's `eslint.config.mjs`, which enforces the mechanical parts.

## Data access: server actions over API routes

Use **Next.js server actions** as the default for all request/response data access
— reads and mutations alike. Reach for an API **route handler** only when a server
action genuinely can't serve the need:

- **Streaming / SSE** — anything backing an `EventSource` (deployment event
  streams, run log streams). Server actions can't stream.
- **Framework / external endpoints** — NextAuth (`/api/auth/...`), inbound
  webhooks, an MCP endpoint, or anything a third party must call by URL.
- **Truly framework-agnostic consumers** — e.g. a reverse proxy.

Everything else (CRUD, list/get, deploy/scale/rollout, secrets, run control, the
editor's load/save via injected capabilities) is a server action.

**Layering.** Keep three thin layers; never skip straight to `fetch`:

```
serverAction (auth boundary)  →  high-level typed client  →  fetch abstraction
  listFolders()                   listFolders()  (no verbs)    @octo/http requestJson()
```

- **`@octo/http`** is the only place that touches `fetch`; it returns a discriminated
  `ActionResult<T>` (`{ ok, data } | { ok, error }`).
- The **high-level client** is a typed, domain-oriented lib (`listFolders()`,
  `deleteSnapshot()`). It must **not expose HTTP verbs** to consumers — paths,
  methods, JSON, base URLs, and secrets stay internal.
- The **server action** is the trust boundary: it authorizes (session for reads,
  write roles for mutations) and delegates to the client. For an **in-process**
  backend (local disk, the in-process run host) the action calls that module
  directly — there is no fetch layer, but the same "no HTTP verbs leak, return
  `ActionResult`" rules apply.

**Return `ActionResult`, don't throw across the boundary.** Next.js redacts thrown
server-action error messages in production, so actions return a result and the
caller (the browser model or an editor capability provider) unwraps it back into
value-or-throw, preserving the real error message.

**Keep the orchestrator/secret URLs server-only.** They live in the action layer
(env vars), never in client code.

## File size

- Keep files **small and focused** — one component/concern per file.
- ESLint warns at **200 lines** (`max-lines`, blanks/comments excluded). When a
  file approaches the limit, extract sub-components, hooks, or data modules
  rather than letting it grow. Tests are exempt.

## Reusable component library

- Generic, reusable presentational primitives live in **`editor/components/ui/`**
  and are re-exported from `components/ui/index.ts`. Import them via the barrel:
  ```ts
  import { PaletteItem } from "@/components/ui";
  ```
- Library components are **stateless/controlled** where possible — state is owned
  by the parent and passed in via props (see `PaletteItem`).
- Feature- or route-specific components live under `editor/app/` (e.g.
  `app/components/`), and compose the `components/ui` primitives.

## State management

- **Small components → `useState`.** Local, ephemeral UI state (input values,
  open/closed toggles) stays in `useState` (see the filter input in `Sidebar`).
- **Large components → reducers.** Components that own non-trivial or shared
  state use a reducer built with
  [`@eetr/react-reducer-utils`](https://www.npmjs.com/package/@eetr/react-reducer-utils).
  Define an action enum, a typed state, a reducer, and bootstrap a provider:
  ```ts
  const { Provider, useContextAccessors } =
    bootstrapProvider<State, ReducerAction<ActionType>>(reducer, initialState);
  ```
  Action payloads go on the `data` field of `ReducerAction`. See
  `editor/app/state/editorState.tsx` (the `EditorShell` reducer).

## UI unit testing

- Tests use **Vitest + React Testing Library** (jsdom). Run with `npm test`
  (single run) or `npm run test:watch`, or `task editor:test` from the repo root.
- Co-locate tests next to the component as `*.test.tsx`.
- Test **behavior**, not implementation: query by role/text/label, drive
  interactions with `@testing-library/user-event`, assert on what the user sees.
- Every reusable `components/ui` primitive should have a unit test. See
  `editor/components/ui/PaletteItem.test.tsx` and
  `editor/app/components/Sidebar.test.tsx`.
