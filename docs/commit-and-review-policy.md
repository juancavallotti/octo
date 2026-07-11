# Commit and Review Policy

## Plans are broken down into commits

- Every implementation plan must be decomposed into a sequence of small, logical commits.
- Each commit is one behavior or one slice of scaffolding — coherent on its own and,
  where practical, individually reviewable and revertable.
- Tests for a behavior live in the same commit as that behavior (see
  [coding-standards.md](coding-standards.md)).
- Use [Conventional Commits](https://www.conventionalcommits.org/) messages
  (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`, …). Release automation
  derives the changelog and version from these (see [release-process.md](release-process.md)).

## Commit atomically; stop for approval

- Agents implement the planned commits one at a time, in dependency order, and
  **stop for the human's approval before each `git commit`**. The default is to
  pause: implement one commit's worth of work, show what changed and that it is
  green, and wait. Do not chain into the next commit unprompted.
- The human may waive this for a given task ("just do the whole sequence") — but
  that is theirs to say, and an unrelated instruction elsewhere is not it.
- Each commit must build and pass its own tests, so the history stays bisectable.
- Do **not** squash the sequence into one large commit "to save time" — the atomic
  history is the point. Reviewing increment by increment is how the reviewer follows
  how each step builds on the last.
- After the commits are in place, present a short summary of the sequence: what each
  commit does and what tests cover it.

## Pushing still requires approval

- Do not run `git push` or open a pull request until the human has reviewed the
  committed history and confirmed.

## Review quality

- Explain behavioral changes, not just file changes.
- Call out test coverage and known risks.
- Keep follow-up work isolated from unrelated cleanup.

## Baseline exception

- The first baseline commit is direct and does not need a pull request.
