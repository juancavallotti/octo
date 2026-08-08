package devrun

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

var secret = []byte("test-install-secret")

// labelShape is the contract a public host label has to satisfy to be a valid DNS
// label and a valid URL host component with no escaping.
var labelShape = regexp.MustCompile(`^[a-z0-9]{8}$`)

// TestDeriveIsStable is the property the whole no-table design rests on: the same
// pair yields the same identifiers, every time, in any process.
func TestDeriveIsStable(t *testing.T) {
	id1 := deriveID(secret, "user-1", "int-1")
	id2 := deriveID(secret, "user-1", "int-1")
	if id1 != id2 {
		t.Fatalf("deriveID is not stable: %q then %q", id1, id2)
	}
	label1 := deriveLabel(secret, "user-1", "int-1")
	label2 := deriveLabel(secret, "user-1", "int-1")
	if label1 != label2 {
		t.Fatalf("deriveLabel is not stable: %q then %q", label1, label2)
	}
	token1 := deriveSidecarToken(secret, "user-1", "int-1")
	token2 := deriveSidecarToken(secret, "user-1", "int-1")
	if token1 != token2 {
		t.Fatalf("deriveSidecarToken is not stable: %q then %q", token1, token2)
	}
}

// TestDeriveIDIsAWellFormedUUID checks the version and variant bits, so anything
// that parses the id gets a true answer about what kind of uuid it is.
func TestDeriveIDIsAWellFormedUUID(t *testing.T) {
	for _, integrationID := range []string{"int-1", "int-2", "", "a-very-long-integration-id-value"} {
		id := deriveID(secret, "user-1", integrationID)
		parsed, err := uuid.Parse(id)
		if err != nil {
			t.Fatalf("deriveID(%q) = %q, not a uuid: %v", integrationID, id, err)
		}
		if got := parsed.Version(); got != 8 {
			t.Errorf("deriveID(%q) version = %d, want 8", integrationID, got)
		}
		if got := parsed.Variant(); got != uuid.RFC4122 {
			t.Errorf("deriveID(%q) variant = %v, want RFC4122", integrationID, got)
		}
	}
}

// TestDeriveIDFitsAKubernetesName guards the one hard limit: octo-dev-{uuid} has to
// stay inside the 63-character DNS-1123 label limit.
func TestDeriveIDFitsAKubernetesName(t *testing.T) {
	name := "octo-dev-" + deriveID(secret, "user-1", "int-1")
	if len(name) > 63 {
		t.Fatalf("workload name %q is %d chars, over the 63-char limit", name, len(name))
	}
}

// TestDeriveLabelShape checks the slug contract over enough inputs that the
// rejection-sampling loop's "draw another round" branch is exercised somewhere.
func TestDeriveLabelShape(t *testing.T) {
	for i := range 500 {
		label := deriveLabel(secret, "user-1", strings.Repeat("x", i%17)+string(rune('a'+i%26)))
		if !labelShape.MatchString(label) {
			t.Fatalf("deriveLabel produced %q, want %v", label, labelShape)
		}
	}
}

// TestSampleLabelDrawsFurtherRounds covers the reason the loop asks for more rounds
// at all: a digest whose bytes are all rejected must not yield a short label. Round 0
// here is entirely unusable, so only the next round can fill the slug.
func TestSampleLabelDrawsFurtherRounds(t *testing.T) {
	rounds := 0
	label := sampleLabel(func(r int) []byte {
		rounds++
		if r == 0 {
			return bytes.Repeat([]byte{0xff}, 32) // every byte at or above rejectAt
		}
		return bytes.Repeat([]byte{byte(r)}, 32)
	})
	if rounds < 2 {
		t.Fatalf("sampleLabel used %d round(s); a fully rejected round must draw another", rounds)
	}
	if !labelShape.MatchString(label) {
		t.Fatalf("sampleLabel produced %q, want %v", label, labelShape)
	}
}

// TestSampleLabelCompletesAPartialRound is the realistic version of the same case: a
// round supplies some of the characters and the next supplies the rest.
func TestSampleLabelCompletesAPartialRound(t *testing.T) {
	label := sampleLabel(func(r int) []byte {
		if r == 0 {
			// Three usable bytes, then nothing but rejects.
			return append([]byte{0, 1, 2}, bytes.Repeat([]byte{0xff}, 29)...)
		}
		return bytes.Repeat([]byte{3}, 32)
	})
	if !labelShape.MatchString(label) {
		t.Fatalf("sampleLabel produced %q, want %v", label, labelShape)
	}
	if want := "abcddddd"; label != want {
		t.Fatalf("sampleLabel = %q, want %q (three chars from round 0, five from round 1)", label, want)
	}
}

// TestDeriveDomainsAreSeparated is why each derivation mixes in a prefix: publishing
// the host label must not leak the digest that authorises commands.
func TestDeriveDomainsAreSeparated(t *testing.T) {
	id := deriveID(secret, "user-1", "int-1")
	label := deriveLabel(secret, "user-1", "int-1")
	token := deriveSidecarToken(secret, "user-1", "int-1")

	// The id and token are both 32 bytes of the same secret over the same pair; only
	// the domain prefix differs. If they matched, the prefixes would not be in play.
	if strings.Contains(strings.ReplaceAll(id, "-", ""), token[:8]) {
		t.Errorf("id %q shares a prefix with sidecar token %q", id, token)
	}
	if strings.HasPrefix(strings.ReplaceAll(id, "-", ""), label) {
		t.Errorf("id %q starts with host label %q", id, label)
	}
}

// TestDeriveSeparatesTheIdentityPair is the NUL separator: ("ab","c") must not hash
// like ("a","bc").
func TestDeriveSeparatesTheIdentityPair(t *testing.T) {
	if a, b := deriveID(secret, "ab", "c"), deriveID(secret, "a", "bc"); a == b {
		t.Errorf("deriveID(ab,c) == deriveID(a,bc) == %q; the pair is not separated", a)
	}
	if a, b := deriveLabel(secret, "ab", "c"), deriveLabel(secret, "a", "bc"); a == b {
		t.Errorf("deriveLabel(ab,c) == deriveLabel(a,bc) == %q; the pair is not separated", a)
	}
}

// TestDeriveIsKeyed is what makes a public dev-run host unguessable: two installs
// with different secrets derive different hosts from the same pair.
func TestDeriveIsKeyed(t *testing.T) {
	other := []byte("a-different-install-secret")
	if a, b := deriveLabel(secret, "user-1", "int-1"), deriveLabel(other, "user-1", "int-1"); a == b {
		t.Errorf("deriveLabel ignores the secret: both installs derive %q", a)
	}
	if a, b := deriveID(secret, "user-1", "int-1"), deriveID(other, "user-1", "int-1"); a == b {
		t.Errorf("deriveID ignores the secret: both installs derive %q", a)
	}
}

// TestDeriveDistinguishesIntegrations is the "two copies of one integration are two
// apps" property — including two that a user might well have named the same.
func TestDeriveDistinguishesIntegrations(t *testing.T) {
	a := deriveLabel(secret, "user-1", "int-1")
	b := deriveLabel(secret, "user-1", "int-2")
	if a == b {
		t.Fatalf("two integrations derive the same host %q", a)
	}
	if u := deriveLabel(secret, "user-2", "int-1"); u == a {
		t.Fatalf("two users derive the same host %q for one integration", a)
	}
}
