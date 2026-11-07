package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/juancavallotti/octo/octopaas/internal/config"
)

// originFile is where pull records which integration a folder came from, beside
// the manifest in .octo/. It is octopaas's own: the manifest is the platform's and
// goes back to it on push, this never leaves the machine.
var originFile = filepath.Join(".octo", "pulled-from.json")

// origin is the integration a folder was pulled from. The id is what push checks,
// because an id belongs to one environment: the same name in another environment
// is another integration. The environment is kept for the person reading the
// refusal, and only when the pull named one, since a key bound to an environment
// acts in it without saying which.
type origin struct {
	Platform      string `json:"platform"`
	IntegrationID string `json:"integrationId"`
	Name          string `json:"name"`
	Environment   string `json:"environment,omitempty"`
}

// originOf describes the integration a command is about to pull, as resolved.
func originOf(r config.Resolved, id, name string) origin {
	return origin{Platform: r.URL, IntegrationID: id, Name: name, Environment: r.EnvironmentHeader()}
}

func writeOrigin(dir string, o origin) error {

	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(dir, originFile)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, append(data, '\n'), 0o644)
}

// readOrigin reads what pull recorded in dir, or nil for a folder nothing was
// pulled into: one written by hand, or by an octopaas older than the record.
func readOrigin(dir string) (*origin, error) {

	data, err := os.ReadFile(filepath.Join(dir, originFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ret origin
	if err := json.Unmarshal(data, &ret); err != nil {
		return nil, fmt.Errorf("read %s: %w", originFile, err)
	}
	return &ret, nil
}

// checkOrigin refuses to push a pulled folder onto an integration other than the
// one it came from, which is what a name means in another environment or on
// another platform.
func checkOrigin(o *origin, r config.Resolved, id, ref string) error {

	if o == nil || o.IntegrationID == "" || o.IntegrationID == id {
		return nil
	}
	where := "the default environment"
	flag := ""
	if o.Environment != "" && o.Environment != config.DefaultEnvironment {
		where = "environment " + o.Environment
		flag = " --environment " + o.Environment
	}
	if o.Platform != "" && o.Platform != r.URL {
		where += " on " + o.Platform
	}
	return usageError{fmt.Errorf(
		"this folder was pulled from %s in %s, and %q here is another integration; push it back where it came from%s, or remove %s to push it here",
		o.Name, where, ref, flagHint(flag), originFile)}
}

func flagHint(flag string) string {
	if flag == "" {
		return ""
	}
	return " (add" + flag + ")"
}
