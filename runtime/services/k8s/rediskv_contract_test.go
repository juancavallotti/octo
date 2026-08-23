package k8s

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The volatile KV wire contract, checked against the other side of it.
//
// The key layout and the two Lua scripts are duplicated in the orchestrator's
// internal/kv/redisrepo.go, in a module that cannot import this one and that this
// one cannot import. Drift between the copies does not fail to compile and does not
// fail any test that exercises a single side: it silently produces two stores that
// disagree about where a value lives or about what a version means, which surfaces
// much later as objects vanishing between the browser and the flow that wrote them.
//
// So this reads the other file and compares the definitions themselves. An earlier
// version had each side hash its own copy against a pinned constant, which was
// weaker in exactly the case that matters: change a script and update the constant
// beside it, and that side stays green while the other drifts. Comparing across the
// two is the only check that cannot pass while they disagree.
//
// It lives in the runtime module rather than the orchestrator's because only one
// side needs to run it, and this is the side whose pods do the writing.

// orchestratorRepo is the other copy, relative to this package's directory.
const orchestratorRepo = "../../../orchestrator/internal/kv/redisrepo.go"

// luaScript captures a backtick-quoted Lua constant — the shape both files declare
// their scripts in.
var luaScript = regexp.MustCompile("(?s)`\\n(local .*?)`")

// keyFormat captures the key layout each side documents, so a reordering of the
// segments is caught alongside a script change.
var keyFormat = regexp.MustCompile(`"octo:kv:\{[^"]*"`)

func TestVolatileContractMatchesTheOrchestrator(t *testing.T) {
	theirs, err := os.ReadFile(filepath.Clean(orchestratorRepo))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The runtime module is consumable on its own, where the other side of
			// the contract is simply not present. Skipping is honest; failing would
			// report a problem with the checkout rather than with the code.
			t.Skipf("%s is not present; nothing to compare against", orchestratorRepo)
		}
		t.Fatalf("reading %s: %v", orchestratorRepo, err)
	}

	ours, err := os.ReadFile("rediskv.go")
	if err != nil {
		t.Fatalf("reading rediskv.go: %v", err)
	}

	oursScripts := luaScript.FindAllStringSubmatch(string(ours), -1)
	theirsScripts := luaScript.FindAllStringSubmatch(string(theirs), -1)
	if len(oursScripts) != 2 || len(theirsScripts) != 2 {
		t.Fatalf("expected two Lua scripts on each side, found %d here and %d in %s "+
			"(did one of them stop being a backtick-quoted constant?)",
			len(oursScripts), len(theirsScripts), orchestratorRepo)
	}
	for i := range oursScripts {
		mine, theirs := strings.TrimSpace(oursScripts[i][1]), strings.TrimSpace(theirsScripts[i][1])
		if mine != theirs {
			t.Errorf("Lua script %d differs between the two modules.\n\n"+
				"Runtime pods and the orchestrator both run these against the same keys, so a "+
				"change to one copy has to land in the other.\n\nthis module:\n%s\n\n%s:\n%s",
				i+1, mine, orchestratorRepo, theirs)
		}
	}

	// Both files document the key layout in a string literal; they have to agree on
	// it for the scripts to be operating on the same keys at all.
	if mine, theirs := keyFormat.FindString(string(ours)), keyFormat.FindString(string(theirs)); mine != theirs {
		t.Errorf("the key layout differs: this module documents %s, %s documents %s",
			mine, orchestratorRepo, theirs)
	}
}

// The comparison above is only worth anything if the documented layout is what the
// code actually produces, which is what this pins on this side. The orchestrator
// pins the same thing on its own.
func TestKeyOfMatchesTheDocumentedLayout(t *testing.T) {
	want := strings.NewReplacer(
		"{deployment}", "dep-1",
		"{namespace}", "user_volatile",
		"{key}", "profile:7",
	).Replace(volatileKeyLayout)

	s := &redisStore{deploymentID: "dep-1"}
	if got := s.keyOf("user_volatile", "profile:7"); got != want {
		t.Errorf("keyOf = %q, but the documented layout renders to %q", got, want)
	}
}

// keyOf is the layout the comparison above pins; this is what it actually produces.
func TestKeyOfLayout(t *testing.T) {
	s := &redisStore{deploymentID: "dep-1"}
	if got := s.keyOf("user_volatile", "profile:7"); got != "octo:kv:dep-1:user_volatile:profile:7" {
		t.Errorf("keyOf = %q", got)
	}
	// The object key takes everything after the namespace, so colons in it are fine
	// — that is what lets the orchestrator split a scanned key at the FIRST colon.
	// A colon in the *namespace* would break that, which is why the store refuses
	// one; see TestVolatileNamespacesRejectColons in the orchestrator.
	if got := s.keyOf("user", "a:b:c"); got != "octo:kv:dep-1:user:a:b:c" {
		t.Errorf("keyOf with colons in the key = %q", got)
	}
}
