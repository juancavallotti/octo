package cmd

import (
	"strings"
	"testing"
)

// A refusal under an API key blames the key's scope, not the account.
func TestAForbiddenKeyIsToldItsScopeIsShort(t *testing.T) {

	status, out := run(t, &fakeAPI{forbidden: "this requires the platform:operator role"}, "deployments", "outdated", "--plain")
	if status == 0 {
		t.Fatalf("status 0, want a failure: %s", out)
	}
	for _, want := range []string{"This requires the platform:operator role", "does not carry a role", "octopaas whoami", "account page"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Ask an administrator") {
		t.Errorf("a key's refusal should not send the caller to an administrator:\n%s", out)
	}
}
