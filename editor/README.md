# Octo — visual editor

The **Octo** visual editor for integrations: a standalone Next.js app
(App Router, TypeScript, Tailwind v4). It is **not** embedded in or served by the
Go binary — it builds and runs independently via npm.

## Commands

```bash
npm install      # install dependencies
npm run dev      # dev server at http://localhost:3000
npm run lint     # ESLint
npm test         # Vitest (single run); npm run test:watch to watch
npm run build    # production build
```

From the repo root you can also use `task editor:install|dev|lint|test|build`.

## Conventions

Read [docs/editor-coding-standards.md](../docs/editor-coding-standards.md) before
changing code: small focused files, reusable primitives in `components/ui/`,
`useState` for small components and `@eetr/react-reducer-utils` reducers for large
ones, and co-located Vitest + React Testing Library unit tests.

## Branding

The logo (`public/octo-logo.png`) and favicon (`app/icon.png`) come from
`docs/assets/octo.png`. Replace those files in place to update the artwork — no
code changes needed.
