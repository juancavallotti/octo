#!/usr/bin/env bash
#
# Interactive bootstrap for a new connector.
#
# Creates runtime/connectors/<name>/<name>.go (plus a test), wires the
# self-registering blank import into the CLI, and tidies + formats the result.
#
# Usage:
#   ./scripts/new-connector.sh [name]
#
# If [name] is omitted the script prompts for it. The name must be a valid Go
# package identifier: lowercase, starting with a letter, letters and digits only.

set -euo pipefail

# Resolve the repository root from this script's location so the task works
# regardless of the caller's working directory.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

readonly CONNECTORS_DIR="${REPO_ROOT}/runtime/connectors"
readonly CLI_MAIN="${REPO_ROOT}/runtime/cli/main.go"
readonly MODULE_PREFIX="github.com/juancavallotti/eip-go/connectors"
readonly NAME_PATTERN='^[a-z][a-z0-9]*$'

err() { printf 'error: %s\n' "$1" >&2; }

prompt_name() {
  # Read from the terminal so the prompt works under `task`/piped stdin.
  local input
  read -r -p "Connector name (lowercase, e.g. http, kafka): " input </dev/tty
  printf '%s' "$input"
}

validate_name() {
  local name="$1"
  if [[ -z "${name}" ]]; then
    err "connector name is required"
    return 1
  fi
  if [[ ! "${name}" =~ ${NAME_PATTERN} ]]; then
    err "invalid name '${name}': must match ${NAME_PATTERN} (lowercase letters/digits, starting with a letter)"
    return 1
  fi
}

write_connector() {
  local name="$1" dir="$2"
  cat >"${dir}/${name}.go" <<EOF
package ${name}

import (
	"context"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// Connector is the ${name} connector.
type Connector struct{}

func init() {
	core.MustRegisterConnector("${name}", func() core.Connector {
		return &Connector{}
	})
}

// Start brings the connector online. Replace this with the real implementation.
func (c *Connector) Start(context.Context, types.ConnectorConfig) error {
	return nil
}

// Stop shuts the connector down. Replace this with the real implementation.
func (c *Connector) Stop(context.Context) error {
	return nil
}
EOF
}

write_test() {
  local name="$1" dir="$2"
  cat >"${dir}/${name}_test.go" <<EOF
package ${name}

import (
	"context"
	"testing"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func TestConnectorStartStop(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

func TestConnectorRegistered(t *testing.T) {
	if _, err := core.DefaultRegistry().New("${name}"); err != nil {
		t.Fatalf("connector %q not registered: %v", "${name}", err)
	}
}
EOF
}

wire_cli_import() {
  local name="$1"
  local import_path="${MODULE_PREFIX}/${name}"

  if grep -q "\"${import_path}\"" "${CLI_MAIN}"; then
    printf 'CLI already imports %s, skipping wiring.\n' "${import_path}"
    return 0
  fi

  # Insert the blank import next to the existing connector imports; gofmt will
  # sort the import group afterwards.
  local marker="${MODULE_PREFIX}/"
  if ! grep -q "${marker}" "${CLI_MAIN}"; then
    err "could not find an existing connector import in ${CLI_MAIN}; add the import manually:"
    err "  _ \"${import_path}\""
    return 0
  fi

  local tmp
  tmp="$(mktemp)"
  awk -v line="	_ \"${import_path}\"" -v marker="${marker}" '
    !done && index($0, marker) { print line; done=1 }
    { print }
  ' "${CLI_MAIN}" >"${tmp}"
  mv "${tmp}" "${CLI_MAIN}"
  printf 'Wired blank import into %s.\n' "${CLI_MAIN#"${REPO_ROOT}/"}"
}

main() {
  local name="${1:-}"
  if [[ -z "${name}" ]]; then
    name="$(prompt_name)"
  fi
  validate_name "${name}"

  local dir="${CONNECTORS_DIR}/${name}"
  if [[ -e "${dir}" ]]; then
    err "connector directory already exists: ${dir#"${REPO_ROOT}/"}"
    exit 1
  fi

  mkdir -p "${dir}"
  write_connector "${name}" "${dir}"
  write_test "${name}" "${dir}"
  printf 'Created %s\n' "${dir#"${REPO_ROOT}/"}/${name}.go"
  printf 'Created %s\n' "${dir#"${REPO_ROOT}/"}/${name}_test.go"

  wire_cli_import "${name}"

  # Format and tidy so the generated code is immediately compliant.
  (cd "${REPO_ROOT}/runtime/connectors" && go fmt ./... >/dev/null)
  (cd "${REPO_ROOT}/runtime/cli" && go fmt ./... >/dev/null && go mod tidy >/dev/null 2>&1 || true)

  printf '\nDone. Next steps:\n'
  printf '  1. Implement Start/Stop in %s\n' "${dir#"${REPO_ROOT}/"}/${name}.go"
  printf '  2. Run: task test\n'
}

main "$@"
