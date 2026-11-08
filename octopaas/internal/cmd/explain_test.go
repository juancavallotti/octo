package cmd

import (
	"strings"
	"testing"
)

// A refusal under an API key is about the key, whichever reason the platform
// gives: a role it does not carry, or roles its owner may no longer give.
func TestAForbiddenKeyIsToldAboutTheKey(t *testing.T) {

	for _, reason := range []string{
		"this requires the platform:operator role",
		"forbidden: this API key's roles are no longer held by its owner; mint a new key",
	} {
		status, out := run(t, &fakeAPI{forbidden: reason}, "deployments", "outdated", "--plain")
		if status == 0 {
			t.Fatalf("status 0, want a failure: %s", out)
		}
		for _, want := range []string{capitalize(reason), "its roles do not cover it, or its owner may no longer give them", "octopaas whoami", "account page"} {
			if !strings.Contains(out, want) {
				t.Errorf("for %q, output lacks %q:\n%s", reason, want, out)
			}
		}
		if strings.Contains(out, "Ask an administrator") {
			t.Errorf("a key's refusal should not send the caller to an administrator:\n%s", out)
		}
	}
}

// With an environment named, a key's refusal may be about where the key is bound,
// so the hint says so instead of blaming its roles alone.
func TestARefusedKeyInANamedEnvironmentMentionsTheBinding(t *testing.T) {

	_, out := run(t, &fakeAPI{forbidden: "this token is bound to environment \"stage\" and cannot act in \"prod\""},
		"--environment", "prod", "deployments", "outdated", "--plain")
	if !strings.Contains(out, "bound to another environment than prod") {
		t.Errorf("the hint does not mention the binding:\n%s", out)
	}
}

// A key that names no environment acts in the one it is bound to, and says so.
func TestAKeyReportsTheEnvironmentItIsBoundTo(t *testing.T) {

	status, out := run(t, &fakeAPI{}, "environments", "current", "-o", "json")
	if status != 0 {
		t.Fatalf("status %d: %s", status, out)
	}
	if !strings.Contains(out, `"environment": "stage"`) || !strings.Contains(out, `"source": "key"`) {
		t.Errorf("want stage, from the key:\n%s", out)
	}
}
