# Octo Editor — Coding Standards

Conventions for the `editor/` module (the **Octo** Next.js visual editor). These
complement the Go [coding-standards.md](coding-standards.md). Keep this in sync
with `editor/eslint.config.mjs`, which enforces the mechanical parts.

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
