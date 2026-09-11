package k8s

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// The bearer token this pod presents to the orchestrator, and how it stays valid.
//
// It is read from a mounted file and renewed at iam: once at start, and after
// that on demand under a lock, so several requests arriving on an expiring token
// queue behind one exchange rather than starting their own.
//
// A renewal is never written back. The mounted token is renewable however long
// ago it expired, so a restart can always start again from it, and a second copy
// on disk would be a credential in one more place for no gain.
//
// Every failure here degrades to "no token" or to the token already held. Nothing
// in this file fails a request.

const (
	// renewLead is how long before expiry the token is traded for a fresh one.
	// Well inside iam's own grace window, so an unlucky pod that misses this still
	// has a second chance rather than a dead credential.
	renewLead = 5 * time.Minute

	// renewTimeout bounds the exchange. It sits in front of a request the caller
	// is waiting on, so it cannot be generous.
	renewTimeout = 10 * time.Second
)

// credentialConfig says where the token comes from.
type credentialConfig struct {
	// Seed is a file holding the token, typically mounted read-only. Empty falls
	// back to Inline.
	Seed string
	// Inline is a token supplied directly, for a run with nothing mounted. Used
	// only when Seed is empty.
	Inline string
	// IAMURL is where a renewal goes. Empty disables renewal, leaving the token
	// to stand until it expires.
	IAMURL string
}

// credential holds the pod's token and renews it as needed. The zero value is a
// usable "no credential", which is what a local run and an unenforced install
// both get.
type credential struct {
	seedPath string
	iamURL   string

	mu        sync.Mutex
	token     string
	mintedAt  time.Time
	expiresAt time.Time
	http      *http.Client
}

// newCredential builds a credential from cfg. It reads no files and makes no
// requests; start does both.
func newCredential(cfg credentialConfig) *credential {
	c := &credential{
		seedPath: strings.TrimSpace(cfg.Seed),
		iamURL:   strings.TrimRight(strings.TrimSpace(cfg.IAMURL), "/"),
		http:     &http.Client{Timeout: renewTimeout},
	}
	if c.seedPath == "" {
		c.set(strings.TrimSpace(cfg.Inline))
	}
	return c
}

// start loads the token and renews it once, so that the seed is spent
// immediately rather than at the first request that needs it — a seed written
// long before the pod started may have only minutes left, or be inside iam's
// grace window rather than valid.
//
// It never fails: a pod that cannot renew keeps what it has, and the orchestrator
// decides whether that is good enough.
func (c *credential) start(ctx context.Context) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.load()
	if c.token == "" || c.iamURL == "" {
		return
	}
	c.renew(ctx)
}

// get returns the token to present, or "" when this pod has none.
//
// The whole renewal happens under the lock. Several requests arriving at once
// while the token is expiring will queue behind one exchange rather than each
// starting their own, which is the behaviour worth having: the alternative is a
// thundering herd at iam every hour, from every pod.
func (c *credential) get(ctx context.Context) string {
	// A nil credential is a pod with none, which is what a local run and an
	// unenforced install both have. Answering rather than panicking keeps every
	// caller free of a check they would otherwise all have to make.
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// The file is consulted every time, not only when the held token is expiring:
	// it can be replaced underneath this process, and a copy held in memory would
	// go on being presented for the best part of an hour.
	c.load()
	if c.token == "" || !c.expiring() {
		return c.token
	}
	c.renew(ctx)
	return c.token
}

// expiring reports whether the held token is close enough to expiry to replace.
// A token whose expiry could not be read is treated as expiring, so a malformed
// one is refreshed rather than presented forever.
func (c *credential) expiring() bool {
	return c.expiresAt.IsZero() || time.Until(c.expiresAt) <= renewLead
}

// load adopts the mounted token when it was minted more recently than the one in
// hand — a rollout replacing it, rather than the copy a renewal came from.
//
// Deliberately not "whichever expires later": a mounted token issued with a
// longer lifetime than a renewal would win every time, and the renewal would
// never take effect.
func (c *credential) load() {
	candidate := readToken(c.seedPath)
	if candidate == "" || candidate == c.token {
		return
	}
	if minted, _ := timesOf(candidate); minted.After(c.mintedAt) {
		c.set(candidate)
	}
}

// readToken reads a token file, answering "" for any reason it cannot.
func readToken(path string) string {
	if path == "" {
		return ""
	}
	//nolint:gosec // G304: the path is the operator's own configuration, and
	// reading it is the whole job.
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("k8s: could not read a token file", "path", path, "error", err)
		}
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// renew trades the held token for a fresh one at iam.
func (c *credential) renew(ctx context.Context) {
	if c.iamURL == "" || c.token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, renewTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.iamURL+"/auth/refresh", nil)
	if err != nil {
		slog.Warn("k8s: could not build the token renewal request", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// Kept rather than dropped. The one we hold may still have minutes left,
		// and iam being briefly unreachable is not a reason to start making
		// unauthenticated requests.
		slog.Warn("k8s: could not renew the orchestrator token", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("k8s: iam refused to renew the orchestrator token",
			"status", resp.StatusCode,
			"hint", "the deployment may need to be rolled to be given a fresh one")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		slog.Warn("k8s: could not read the renewed orchestrator token", "error", err)
		return
	}
	c.set(strings.TrimSpace(body.Token))
}

// set records a token and reads its timestamps out of it.
func (c *credential) set(token string) {
	if token == "" {
		return
	}
	c.token = token
	c.mintedAt, c.expiresAt = timesOf(token)
}

// timesOf reads `iat` and `exp` out of a JWT without verifying it.
//
// Not a security check and not pretending to be one: this token is ours, and it
// is being presented rather than trusted. All that is wanted is which of two
// tokens is newer, and when the held one should be replaced. Anything unreadable
// reports the zero time, which loses every comparison and reads as "expiring".
//
// A token with no `iat` falls back to its expiry for ordering, which ranks two
// tokens of equal lifetime identically to their issue times.
func timesOf(token string) (minted, expires time.Time) {
	// header.payload.signature — a compact JWS has exactly three parts.
	const compactParts = 3
	parts := strings.Split(token, ".")
	if len(parts) != compactParts {
		return time.Time{}, time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, time.Time{}
	}
	var claims struct {
		Iat int64 `json:"iat"`
		Exp int64 `json:"exp"`
	}
	if err := json.NewDecoder(bytes.NewReader(payload)).Decode(&claims); err != nil {
		return time.Time{}, time.Time{}
	}
	if claims.Exp != 0 {
		expires = time.Unix(claims.Exp, 0)
	}
	minted = expires
	if claims.Iat != 0 {
		minted = time.Unix(claims.Iat, 0)
	}
	return minted, expires
}

// authorize attaches the pod's credential to req, when it has one.
func (c *credential) authorize(req *http.Request) {
	if token := c.get(req.Context()); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// String keeps a credential out of a log line that formats the struct it is on.
func (c *credential) String() string {
	if c == nil {
		return "no credential"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		return "no credential"
	}
	return fmt.Sprintf("credential(expires %s)", c.expiresAt.UTC().Format(time.RFC3339))
}
