# EIP Go

`eip-go` is a repository that will contain multiple stacks, including a Go workspace for a cloud-native integration runtime.

## Layout

- `runtime/`: active Go workspace for the runtime engine and CLI.
- `docs/`: coding standards, lint policy, review policy, and release process.
- future top-level folders: `terraform/`, `ui/`, and other stack-specific modules.

## Working rules

Read [AGENTS.md](AGENTS.md) before changing code.
Read [docs/coding-standards.md](docs/coding-standards.md) for code style and design rules.
Read [docs/linting-policy.md](docs/linting-policy.md) for lint expectations.
Read [docs/release-process.md](docs/release-process.md) before release-related work.

The Go runtime workspace lives under [runtime/](runtime/).

## Tasks

- `task fmt`
- `task test`
- `task build`
- `task tidy`
- `task lint-strict`
- `task policy-check`
- `task release-check`
