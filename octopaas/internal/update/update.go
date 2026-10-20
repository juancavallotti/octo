// Package update finds out whether a newer octopaas has been released, and
// installs it over the running binary.
//
// Releases are the repository's GitHub releases: every tag carries an
// octopaas_<os>_<arch> archive and a checksums.txt covering it. Checking costs one
// call to GitHub's API and is cached, so the notice a command prints after it
// finishes asks GitHub at most once a day.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CheckEvery is how stale the cached answer may get before it is asked again.
const CheckEvery = 24 * time.Hour

// Source is where releases come from. Default is the repository's GitHub
// releases; tests point it at a fake.
type Source struct {
	// API answers GET {API}/releases/latest the way GitHub does.
	API string
	// Download serves {Download}/{tag}/{asset}.
	Download string
	HTTP     *http.Client
}

// Default is the octo repository on GitHub, called as userAgent.
func Default(userAgent string) Source {
	return Source{
		API:      "https://api.github.com/repos/juancavallotti/octo",
		Download: "https://github.com/juancavallotti/octo/releases/download",
		HTTP:     &http.Client{Timeout: 2 * time.Minute, Transport: agent{name: userAgent, next: http.DefaultTransport}},
	}
}

type agent struct {
	name string
	next http.RoundTripper
}

func (a agent) RoundTrip(r *http.Request) (*http.Response, error) {

	r = r.Clone(r.Context())
	r.Header.Set("User-Agent", a.name)
	return a.next.RoundTrip(r)
}

// Latest is the newest release's tag, such as v0.12.0.
func (s Source) Latest(ctx context.Context) (string, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.API+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("ask GitHub for the latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %d for the latest release", resp.StatusCode)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil || release.TagName == "" {
		return "", errors.New("GitHub's latest release names no tag")
	}
	return release.TagName, nil
}

// Newer says whether release is a later version than current. Either may carry
// a leading v; anything that is not a version is never newer.
func Newer(current, release string) bool {

	a, okA := parse(current)
	b, okB := parse(release)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if b[i] != a[i] {
			return b[i] > a[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {

	var ret [3]int
	//a pre-release or build suffix (v1.2.3-rc1) reads as its base version; we
	//only ever compare against published releases, which carry none.
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return ret, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return ret, false
		}
		ret[i] = n
	}
	return ret, true
}

// State is the cached answer, in update.yaml beside the config.
type State struct {
	CheckedAt time.Time `yaml:"checkedAt"`
	Latest    string    `yaml:"latest"`
}

func statePath(home string) string {
	return filepath.Join(home, "update.yaml")
}

// Cached is the last answer, and whether it is still fresh at now.
func Cached(home string, now time.Time) (State, bool) {

	var ret State
	data, err := os.ReadFile(statePath(home))
	if err != nil || yaml.Unmarshal(data, &ret) != nil {
		return State{}, false
	}
	return ret, now.Sub(ret.CheckedAt) < CheckEvery
}

// Refresh asks the source for the latest release and caches the answer.
func Refresh(ctx context.Context, s Source, home string, now time.Time) (State, error) {

	tag, err := s.Latest(ctx)
	if err != nil {
		return State{}, err
	}
	ret := State{CheckedAt: now.UTC(), Latest: tag}
	return ret, Record(home, ret)
}

// Record caches an answer already in hand, such as one `upgrade` just got.
func Record(home string, state State) error {

	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(statePath(home), data, 0o600)
}
