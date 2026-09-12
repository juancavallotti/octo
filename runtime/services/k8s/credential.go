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

// The platform token this pod presents, and how it stays valid.
//
// It is read from a mounted file and renewed at iam: once at start, and from then
// on by a daemon that trades it in ahead of expiry. Reading it is therefore a
// lock and a field — no file, no network — which is what lets it be read on every
// expression evaluation as well as on every outbound request.
//
// The daemon is the reason the read is cheap, and the cheap read is the reason
// the daemon exists. Renewing where it is used would mean either a file read and
// possibly an HTTP exchange inside expression evaluation, or a token refreshed
// only when something happened to ask — and an integration that goes quiet for an
// hour and then wakes on a queue message is exactly the case that has to work.
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

	// renewRetry is how long the daemon waits after a renewal it could not
	// complete. Short relative to renewLead, so a few attempts fit inside the lead
	// and an iam that is briefly unreachable costs nothing at all.
	renewRetry = 30 * time.Second

	// renewFloor bounds how often the daemon may wake, whatever the arithmetic
	// says. A token minted with a lifetime shorter than renewLead is always "due"
	// — without a floor that is a renewal loop as fast as the network allows.
	renewFloor = time.Minute

	// seedPoll is how often the mounted file is re-read while nothing else is
	// happening. A rollout replaces it under a running pod, and adopting the new
	// one promptly is the difference between a deployment that was re-credentialed
	// and one that finds out when its own token runs out.
	seedPoll = 5 * time.Minute
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
//
// It then leaves a daemon behind to keep it that way, which is what makes every
// later read a field read. The daemon stops with ctx.
func (c *credential) start(ctx context.Context) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.load()
	seeded := c.token != "" && c.iamURL != ""
	if seeded {
		c.renew(ctx)
	}
	c.mu.Unlock()

	if seeded {
		go c.maintain(ctx)
	}
}

// maintain renews the token ahead of expiry, for as long as ctx lives.
//
// It sleeps until the held token is due rather than ticking on a fixed interval,
// so a pod doing nothing all night wakes a handful of times rather than
// hundreds — and wakes with a valid credential either way, which is the point:
// a queue message arriving after eight idle hours must find a token, not a
// renewal it has to wait for.
//
// A failed renewal is retried soon rather than waited out, because the lead is
// the budget for exactly that.
func (c *credential) maintain(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.sleepFor()):
		}

		c.refresh(ctx)
	}
}

// refresh is one pass of the daemon: adopt a replaced file, then renew if what we
// hold is near expiry.
//
// The file comes first because a rollout mints a new token and mounts it, and
// adopting that is both cheaper than an exchange and more correct — it is the
// token this deployment is now meant to present.
func (c *credential) refresh(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.load()
	if c.token != "" && c.expiring() {
		c.renew(ctx)
	}
}

// sleepFor is how long until the held token wants attention: its renewal lead,
// the retry interval when it is already overdue, or the seed poll when there is
// nothing to renew — never less than the floor.
func (c *credential) sleepFor() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.expiresAt.IsZero() {
		return seedPoll
	}
	due := time.Until(c.expiresAt) - renewLead
	if due < renewFloor {
		// Already due, or nearly. Something is wrong — a renewal that failed, or a
		// token minted shorter than the lead — and the answer to both is to try
		// again shortly rather than to spin.
		return renewRetry
	}
	if due > seedPoll {
		// Long-lived token: still look at the file periodically, so a rollout is
		// noticed long before the renewal would have noticed it.
		return seedPoll
	}
	return due
}

// get returns the token to present, or "" when this pod has none.
//
// A lock and a field. Keeping it valid is the daemon's job (see maintain), which
// is what lets this be called on every outbound request and on every expression
// that reads the variable it backs, without either becoming a file read or an
// exchange with iam.
func (c *credential) get() string {
	// A nil credential is a pod with none, which is what a local run and an
	// unenforced install both have. Answering rather than panicking keeps every
	// caller free of a check they would otherwise all have to make.
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
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
//
// A file that is simply absent is the ordinary case for an install with no iam,
// and says nothing. A file that exists and cannot be read is a misconfiguration
// — the wrong mode on a mount, most likely — and it is reported, because the
// symptom otherwise is a pod that authenticates as nobody for a reason nothing
// prints.
func readToken(path string) string {
	if path == "" {
		return ""
	}
	//nolint:gosec // G304: the path is the operator's own configuration, and
	// reading it is the whole job.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("k8s: no token file", "path", path)
		} else {
			slog.Error("k8s: a token is mounted but cannot be read, so this pod has none",
				"path", path, "error", err)
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
	if token := c.get(); token != "" {
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
