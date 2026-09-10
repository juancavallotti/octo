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
// The orchestrator mints one for each deployment when it creates it, on the
// authority of whoever deployed it, and mounts it as a file. A file and not an
// environment variable, deliberately: an env var is visible in the pod's own spec
// to anybody who can describe it, is inherited by every child process, and cannot
// be replaced without restarting the container. A mounted file is none of those.
//
// The token is short-lived — an hour — so it is renewed here rather than
// re-mounted, by trading it for a fresh one at iam. That happens on demand under
// a lock rather than on a timer: a pod that is doing nothing needs no credential,
// and the request that does need one can afford the round trip that gets it.
//
// Everything degrades to "no token", because an installation that is not
// enforcing does not need one and must keep working exactly as it did.

const (
	// renewLead is how long before expiry the token is traded for a fresh one.
	// Well inside iam's own grace window, so an unlucky pod that misses this still
	// has a second chance rather than a dead credential.
	renewLead = 5 * time.Minute

	// renewTimeout bounds the exchange. It sits in front of a request the caller
	// is waiting on, so it cannot be generous.
	renewTimeout = 10 * time.Second
)

// credential holds the pod's token and renews it as needed. The zero value is a
// usable "no credential", which is what a local run and an unenforced install
// both get.
type credential struct {
	// path is the mounted token file, re-read when the token is missing or has
	// been replaced underneath us. Empty when the token came from the environment.
	path string
	// iamURL is where a renewal goes. Empty disables renewal, leaving whatever was
	// mounted to stand until it expires.
	iamURL string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	http      *http.Client
}

// newCredential builds the pod's credential from its environment.
//
// The file wins over the variable: a deployment gets the file, and the variable
// is what a `task runtime:run` on somebody's machine can set without one.
func newCredential(path, inline, iamURL string) *credential {
	c := &credential{
		path:   strings.TrimSpace(path),
		iamURL: strings.TrimRight(strings.TrimSpace(iamURL), "/"),
		http:   &http.Client{Timeout: renewTimeout},
	}
	if c.path == "" {
		c.set(strings.TrimSpace(inline))
	}
	return c
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

	// The file is consulted every time, not only when the held token is expiring.
	// The orchestrator can replace it — a rollout, a rotation, a revocation — and
	// a pod that only looked when its own copy was about to run out would keep
	// presenting a withdrawn credential for the best part of an hour.
	c.readFile()
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

// readFile loads the mounted token, if there is one and it has changed.
func (c *credential) readFile() {
	if c.path == "" {
		return
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		// Not an error worth failing a request over: whatever is held stands, and
		// the orchestrator will say whether that was acceptable.
		slog.Debug("k8s: could not read the orchestrator token file",
			"path", c.path, "error", err)
		return
	}

	mounted := strings.TrimSpace(string(raw))
	if mounted == "" || mounted == c.token {
		return
	}
	// Adopted only when it outlasts what is held. Otherwise a renewal would be
	// undone on the very next call — the file still has the older token the
	// orchestrator mounted, and taking it back would mean renewing again, and
	// again, once per request.
	if expiryOf(mounted).After(c.expiresAt) {
		c.set(mounted)
	}
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

// set records a token and reads the expiry out of it.
func (c *credential) set(token string) {
	if token == "" {
		return
	}
	c.token = token
	c.expiresAt = expiryOf(token)
}

// expiryOf reads `exp` out of a JWT without verifying it.
//
// Not a security check and not pretending to be one: the token is ours, we are
// presenting it rather than trusting it, and all that is wanted is "when should
// this be replaced". Whether it is genuine is the orchestrator's question, and it
// has the keys to answer it. A token whose expiry cannot be read reports the zero
// time, which reads as "replace it".
func expiryOf(token string) time.Time {
	// header.payload.signature — a compact JWS has exactly three parts.
	const compactParts = 3
	parts := strings.Split(token, ".")
	if len(parts) != compactParts {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.NewDecoder(bytes.NewReader(payload)).Decode(&claims); err != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
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
