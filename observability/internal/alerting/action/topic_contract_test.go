package action

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The topic subject contract, checked against the other side of it.
//
// scopedSubjectFormat is a second copy of a format the runtime owns and does not
// export, in runtime/services/k8s/topics.go. Drift between the copies does not
// fail to compile and does not fail any test that exercises a single side: the
// runtime renames its scoping segment, this service keeps publishing to the old
// subject, and the symptom is that alerts stop arriving — with every log line on
// both sides reporting success, because a NATS publish to a subject nobody
// subscribes to is not an error.
//
// So this reads the runtime's source and compares the format itself. It lives on
// this side because this is the side that breaks silently: the runtime notices
// nothing at all, since from its point of view nothing has changed.
//
// The same device podstats/wire_contract_test.go uses for the stats wire types,
// for the same reason.

// The runtime's file, relative to this package's directory.
const runtimeTopics = "../../../../runtime/services/k8s/topics.go"

// subjectFormat captures the Sprintf format the runtime scopes a topic with.
var subjectFormat = regexp.MustCompile(`fmt\.Sprintf\("(octo\.[^"]+)",\s*t\.deploymentID,\s*subject\)`)

// systemPrefixDecl captures the prefix that opts a subject out of scoping.
var systemPrefixDecl = regexp.MustCompile(`const systemPrefix = "([^"]+)"`)

func readRuntimeTopics(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile(runtimeTopics)
	if err != nil {
		t.Fatalf("read the runtime's topic scoping from %s: %v\n"+
			"if the file moved, this contract test has to move with it", runtimeTopics, err)
	}
	return string(source)
}

// The format this package publishes with must be the format the runtime
// subscribes with, or an alert goes nowhere and says it succeeded.
func TestScopedSubjectMatchesTheRuntime(t *testing.T) {
	match := subjectFormat.FindStringSubmatch(readRuntimeTopics(t))
	if match == nil {
		t.Fatalf("could not find the topic scoping format in %s; "+
			"the runtime changed how it builds a subject and this copy is now unverified", runtimeTopics)
	}
	if match[1] != scopedSubjectFormat {
		t.Errorf("the runtime scopes topics as %q and this service publishes as %q",
			match[1], scopedSubjectFormat)
	}
}

// And the rendered subject has to look like one a deployment actually receives.
func TestARenderedSubjectIsWhatADeploymentSubscribesTo(t *testing.T) {
	got := fmt.Sprintf(scopedSubjectFormat, "d1", "alerts")
	if want := "octo.d1.t.alerts"; got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
	// The "t" segment is what keeps topics apart from queues, which use "q".
	// Publishing an alert onto the queue plane would deliver it to one member of
	// a group rather than to every subscriber.
	if !strings.Contains(got, ".t.") {
		t.Errorf("subject %q is not on the topic plane", got)
	}
}

// The runtime refuses Subscribe on a system: subject on purpose — that plane
// carries every deployment's logs and traces. This asserts the prefix has not
// changed underneath the validation that refuses to publish to one, because a
// stale check here would let a watch be saved that can never be received.
func TestSystemPrefixMatchesTheRuntime(t *testing.T) {
	match := systemPrefixDecl.FindStringSubmatch(readRuntimeTopics(t))
	if match == nil {
		t.Fatalf("could not find systemPrefix in %s", runtimeTopics)
	}
	if err := validSubject(match[1] + "internal.alerts"); err == nil {
		t.Errorf("a %q subject was accepted; a deployment cannot subscribe to one", match[1])
	}
}
