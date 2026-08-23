package expr

import (
	"regexp"
	"testing"
)

// The shape a strict parser will accept: 8-4-4-4-12 lowercase hex, version 4 in
// the thirteenth character and the RFC variant in the seventeenth.
var uuidV4Pattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestUUIDIsAValidVersion4(t *testing.T) {
	got := evalString(t, "uuid()")
	if !uuidV4Pattern.MatchString(got) {
		t.Errorf("uuid() = %q, which is not a version 4 UUID", got)
	}
}

// The whole point of the function: a fresh value every evaluation, including
// twice inside one expression.
func TestUUIDIsDifferentEveryTime(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		got := evalString(t, "uuid()")
		if seen[got] {
			t.Fatalf("uuid() repeated %q after %d calls", got, i)
		}
		seen[got] = true
	}

	if same := evalString(t, `uuid() == uuid() ? "same" : "different"`); same != "different" {
		t.Error("two calls in one expression produced the same value")
	}
}

// It composes like any other string, which is most of how it will be used: a
// correlation id on an outbound request, a synthetic key for a record without one.
func TestUUIDComposesWithTheMessage(t *testing.T) {
	got := evalStringWith(t, `"order-" + body.customer + "-" + uuid()`,
		map[string]any{"customer": "ada"})
	if len(got) != len("order-ada-")+36 || got[:10] != "order-ada-" {
		t.Errorf("composed = %q, want the prefix and a UUID after it", got)
	}
	if !uuidV4Pattern.MatchString(got[10:]) {
		t.Errorf("composed = %q, whose tail is not a UUID", got)
	}
}
