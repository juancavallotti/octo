package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/internal/schemafmt"
	"github.com/juancavallotti/octo/runtime/services"
)

// The module's commands reach the CLI through its own registration, so a binary
// that carries the module carries them and one that does not, does not.
func TestModuleRegistersItsCommands(t *testing.T) {
	for _, name := range []string{"openapi", "verify-platform-api"} {
		if _, ok := services.LookupCommand(name); !ok {
			t.Errorf("the api module did not register %q", name)
		}
	}
}

// Every command documents itself, because one absent from --help may as well not
// exist.
func TestCommandsDocumentThemselves(t *testing.T) {
	for _, name := range []string{"openapi", "verify-platform-api"} {
		cmd, ok := services.LookupCommand(name)
		if !ok {
			t.Fatalf("%q is not registered", name)
		}
		if !strings.Contains(cmd.Usage(), name) {
			t.Errorf("%q's help section does not name the command:\n%s", name, cmd.Usage())
		}
	}
}

// The default is YAML, verbatim, comments included: the prose is what an
// implementer is reading the document for.
func TestContractDefaultsToAnnotatedYAML(t *testing.T) {
	data, err := document(schemafmt.FormatYAML)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if !strings.Contains(string(data), "# READ THIS FIRST") {
		t.Fatal("the YAML rendering lost the document's comments")
	}
	if !strings.Contains(string(data), "openapi: 3.1.0") {
		t.Fatal("the YAML rendering is not the contract")
	}
}

// JSON is for tooling that will not read YAML, so it has to be real JSON.
func TestContractAsJSON(t *testing.T) {
	data, err := document(schemafmt.FormatJSON)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the JSON rendering does not parse: %v", err)
	}
	if doc["paths"] == nil {
		t.Fatal("the JSON rendering has no paths")
	}
}

func TestContractRejectsAnUnknownFormat(t *testing.T) {
	if _, err := document("toml"); err == nil {
		t.Fatal("document err = nil, want a failure naming the formats")
	}
}

// --out writes the contract where it was asked to, which is how the docs build
// takes its copy.
func TestOpenAPICommandWritesToAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform-api.yaml")
	cmd, ok := services.LookupCommand("openapi")
	if !ok {
		t.Fatal("openapi is not registered")
	}
	if err := cmd.Run([]string{"--out", path}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	written, err := os.ReadFile(path) //nolint:gosec // a path this test just made
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "openapi: 3.1.0") {
		t.Fatal("the written file is not the contract")
	}
}

// The verification command exits non-zero when the contract is not satisfied,
// which is what makes it usable in a pipeline.
func TestVerifyCommandFailsOnABrokenImplementation(t *testing.T) {
	f := fullBackend(t, fastVerify())
	f.breaks("POST /v1/queues/{subject}/receive", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent) // answers instantly instead of waiting
	})
	t.Setenv(URLEnvVar, f.url())

	cmd, ok := services.LookupCommand("verify-platform-api")
	if !ok {
		t.Fatal("verify-platform-api is not registered")
	}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("Run err = nil against a broken implementation, want a non-zero exit")
	}
}

// And zero when it is satisfied.
func TestVerifyCommandPassesACorrectImplementation(t *testing.T) {
	f := fullBackend(t, fastVerify())
	cmd, _ := services.LookupCommand("verify-platform-api")
	if err := cmd.Run([]string{"--json", f.url()}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
