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

## Three gates: the plan, the end-to-end test plan, and the push

The gates exist to protect what is **irreversible or outward-facing**. A local
commit on a feature branch is neither, so it is not gated. Approving commits one
at a time stopped being practical once the codebase grew to where a single change
spans the runtime, the orchestrator and the apps.

- **The plan is approved before the work starts.** The commit *sequence* is agreed
  up front — that is where the review value is, and it is what keeps the history
  atomic.
- **Then go to the end.** Implement and commit the whole agreed sequence without
  stopping between commits. Nothing mid-sequence is a gate — not an architectural
  contract change, not a commit you are unsure about. Those are **called out** when
  the work is presented: which commits change a contract (a new extension point, a
  schema or migration, a public runtime type), and where you are not confident and
  why. The one reason to stop early is a design question the plan did not settle
  and that cannot be answered from the code; that is the plan being incomplete, not
  a commit gate.
- **Before any pull request, stop and plan the end-to-end test.** Unit tests and
  CI prove the pieces; they do not prove the change works in the running system.
  Once the sequence is committed, present the branch (what each commit does and
  what covers it) together with an end-to-end test plan:
  - the user-visible behaviours the change is supposed to produce, and the ones it
    must not break;
  - the environment that exercises them — the native platform stack, the local
    k3d cluster, a `dolphin` suite, the desktop app — and why that one;
  - the concrete steps and the observable result that counts as a pass;
  - what the plan cannot cover, and the steps that need a human (sign-in, a real
    provider credential).

  Agree the plan, run it, and report the result faithfully, failures included.
- **`git push` and opening a pull request are approved.** Outward-facing, and the
  point where CI, CodeRabbit and the stack tooling engage anyway. Every merge is
  approved too, by name.

Whatever the pacing:

- **The branch is green, not each commit.** This used to require every commit to
  build and pass its own tests, for bisectability. That was written when work
  arrived one reviewed commit at a time; it does not survive how the work is
  actually done now, which is a whole agreed sequence implemented in one pass and
  reviewed as a branch. Holding each intermediate commit green means reordering
  real work to suit it — a schema and its first reader in one commit because
  neither compiles alone, a test written against an interface two commits later —
  and that is the tail wagging the dog. Bisect the merges.
- Commits are still one coherent step each, and are still not squashed. That is
  about **reading** the change, which is where their value actually was: reviewing
  increment by increment is how the reviewer follows how each step builds on the
  last. A commit that does two unrelated things is still wrong; a commit that does
  one thing and does not compile on its own is fine.

## Review quality

- Explain behavioral changes, not just file changes.
- Call out test coverage and known risks.
- Keep follow-up work isolated from unrelated cleanup.
