# Coding Standards

## Core rules

- Keep `types` dependency-free.
- Keep `core` focused on runtime contracts, orchestration, and config loading.
- Keep connectors isolated and self-registering.
- Prefer explicit dependencies over global state, but allow registry bootstrapping at startup.
- Use `context.Context` for all long-running operations.
- Return wrapped errors with enough context to diagnose failures.

## Go style

- Keep packages small and purpose-driven.
- Avoid unnecessary abstractions.
- Prefer table-driven tests.
- Use short, descriptive names.
- Do not add new dependencies unless they solve a real problem.
