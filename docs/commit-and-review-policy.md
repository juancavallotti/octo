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

## Two gates: the plan, and the push

The gate exists to protect what is **irreversible or outward-facing**. A local
commit on a feature branch is neither, so it is not gated. Approving each one in
turn was the largest source of round-trips on work whose shape was already agreed.

- **The plan is approved before the work starts.** The commit *sequence* is agreed
  up front — that is where the review value is, and it is what keeps the history
  atomic.
- **`git push` and opening a pull request are approved.** Outward-facing, and the
  point where CI, CodeRabbit and the stack tooling engage anyway.
- **In between, chain the approved sequence.** Implement and commit it, then
  present the whole thing: what each commit does and what covers it.

Two cases still stop for approval mid-sequence, and only two:

- A commit that changes an **architectural contract** — a new extension point, a
  schema or migration, a public runtime type.
- **Any commit you are not confident about.** "I am unsure" stops. That one is on
  the agent to notice.

Whatever the pacing:

- Each commit must build and pass its own tests, so the history stays bisectable.
- Do **not** squash the sequence into one large commit "to save time" — the atomic
  history is the point. Reviewing increment by increment is how the reviewer follows
  how each step builds on the last.

## Review quality

- Explain behavioral changes, not just file changes.
- Call out test coverage and known risks.
- Keep follow-up work isolated from unrelated cleanup.
