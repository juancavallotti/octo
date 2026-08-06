package devrun

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
)

// Every identifier a dev run has is derived from the immutable pair
// (user id, integration id) by keyed hash. That is what removes the bookkeeping:
// nothing is minted, so nothing has to be stored to be found again, and any
// orchestrator replica computes the same answers from the same two strings.
//
// Two properties follow from keying on ids rather than on anything editable, and
// both are load-bearing:
//
//   - **A rename never moves the public URL.** A webhook registered with Stripe or
//     GitHub against a dev run's hostname keeps working across a rename, a stop, a
//     restart, and an orchestrator redeploy.
//   - **Two integrations are always two apps**, even when one is a copy of the
//     other or they carry the same display name.
//
// The HMAC key is a per-install secret. Without one, a dev run's public host would
// be a pure function of two values an outsider can often guess — and the hostname
// is the only thing guarding a publicly reachable dev run. This mirrors
// deriveNamespace (packages/run-host/src/namespace.ts), which keys on the half of
// the namespace that never leaves the server, for the same reason.
const (
	// hostLabelLen is the length of a dev run's public host label. Eight characters
	// of the slug alphabet is ~2.8e12 values, and matches run-host's namespace slug
	// so the two hosts look alike to a user who uses both.
	hostLabelLen = 8
	// labelAlphabet is lowercase alphanumerics only, so a label is valid as a DNS
	// label and as a URL host component with no escaping.
	labelAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	// rejectAt is the largest multiple of the alphabet size that fits in a byte.
	// Bytes at or above it are discarded rather than folded, so every character is
	// drawn uniformly instead of biasing the first 256%36 letters.
	rejectAt = 256 - (256 % len(labelAlphabet))

	// Domain separation. The three derivations share one secret and one input pair,
	// so each mixes in a distinct prefix; without it, publishing the host label
	// would leak a digest that also authorises commands.
	idDomain      = "devrun:"
	hostDomain    = "host:"
	sidecarDomain = "sidecar:"

	// sidecarTokenBytes is how much of the digest becomes the sidecar bearer. 32
	// bytes is the whole SHA-256 output.
	sidecarTokenBytes = 32
)

// deriveID returns the dev-run uuid for (userID, integrationID). It names every
// Kubernetes object the run owns — octo-dev-{uuid} — exactly as a deployment id
// names octo-dep-{id}, so the two kinds of workload read alike in kubectl output.
//
// A concurrent Run from a second editor tab computes this same value and so the
// same object name, which is what makes the API server the arbiter of uniqueness:
// one Create wins, the other gets AlreadyExists and attaches. A UNIQUE column
// checked by two orchestrator replicas would have a read-modify-write window; this
// has none.
func deriveID(secret []byte, userID, integrationID string) string {
	sum := digest(secret, idDomain, userID, integrationID, 0)
	var raw uuid.UUID
	copy(raw[:], sum[:len(raw)])
	// Stamp RFC 9562 version 8 (custom, which is what a keyed-hash uuid is) and the
	// RFC variant, so the result is a well-formed uuid rather than 16 arbitrary bytes
	// wearing a uuid's shape. Anything that parses it — a label validator, a log
	// scraper, a human — then gets a real answer about what kind it is.
	raw[6] = (raw[6] & 0x0f) | 0x80
	raw[8] = (raw[8] & 0x3f) | 0x80
	return raw.String()
}

// deriveLabel returns the 8-character public host label for (userID, integrationID),
// the {label} in {label}.{baseDomain}.
func deriveLabel(secret []byte, userID, integrationID string) string {
	return sampleLabel(func(round int) []byte {
		return digest(secret, hostDomain, userID, integrationID, round)
	})
}

// sampleLabel draws hostLabelLen characters of the slug alphabet from successive
// digest rounds, rejecting biased bytes.
//
// One 32-byte digest all but always yields eight usable characters (a byte is
// rejected ~1.6% of the time), but "all but always" is not "always" — so it keeps
// asking for further rounds until it has eight. A short label would be a different,
// still-valid hostname, which is the kind of bug that appears once in fifty thousand
// runs and is then impossible to reproduce.
//
// The digest arrives through a function so that "a round that came up short" is
// directly testable, rather than being a branch that only a lucky input reaches.
func sampleLabel(round func(int) []byte) string {
	out := make([]byte, 0, hostLabelLen)
	for r := 0; len(out) < hostLabelLen; r++ {
		for _, b := range round(r) {
			if len(out) == hostLabelLen {
				break
			}
			// int(b), not b: len() yields a typed int, so rejectAt is an int constant.
			if int(b) >= rejectAt {
				continue
			}
			out = append(out, labelAlphabet[int(b)%len(labelAlphabet)])
		}
	}
	return string(out)
}

// deriveSidecarToken returns the bearer the orchestrator commands this dev run's
// sidecar with.
//
// Derived, where the dev-run token that authorises the sidecar's *inbound* bundle
// pull is randomly minted. The asymmetry is not an oversight — it follows from who
// has to verify:
//
//   - Inbound, the orchestrator verifies. It can read the workload's annotation, so
//     the token can be random and only its hash recorded; a leaked one stops working
//     the moment the pod goes away.
//   - Outbound, the *sidecar* verifies, and a sidecar cannot read annotations. The
//     shared secret therefore has to be reproducible by whichever replica handles
//     the next reload — which is the entire problem this feature exists to fix. A
//     minted one would work only on the replica that created the pod.
func deriveSidecarToken(secret []byte, userID, integrationID string) string {
	sum := digest(secret, sidecarDomain, userID, integrationID, 0)
	return base64.RawURLEncoding.EncodeToString(sum[:sidecarTokenBytes])
}

// digest HMACs the domain-separated identity pair for one round.
//
// The NUL separator between the two ids matters: joined without it, ("ab", "c") and
// ("a", "bc") would hash alike. Both halves are opaque ids rather than user text, so
// this is defence in depth rather than a live risk — but it costs one byte.
func digest(secret []byte, domain, userID, integrationID string, round int) []byte {
	mac := hmac.New(sha256.New, secret)
	// hash.Hash's Write never returns an error, which is why the interface documents
	// it; the returns are discarded rather than checked into noise.
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte(userID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(integrationID))
	_, _ = fmt.Fprintf(mac, ":%d", round)
	return mac.Sum(nil)
}
