// Package credentials keeps what `octopaas auth login` earns: the OAuth client
// octopaas registered with each identity provider, and the tokens each profile
// signed in with.
//
// It lives in credentials.yaml, beside config.yaml and never inside it, so the
// config can be shown, shared and edited without a session riding along.
package credentials

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Client is octopaas as registered with one identity provider (RFC 7591). It is
// a public client: there is no secret, PKCE stands in for one.
type Client struct {
	ClientID    string `yaml:"clientId"`
	RedirectURI string `yaml:"redirectUri"`
	// Scope is what the client registered for, space-separated.
	Scope        string    `yaml:"scope"`
	RegisteredAt time.Time `yaml:"registeredAt"`
}

// Session is one profile signed in: the tokens, and what is needed to refresh
// them without discovering everything again.
type Session struct {
	Issuer        string    `yaml:"issuer"`
	Resource      string    `yaml:"resource"`
	ClientID      string    `yaml:"clientId"`
	TokenEndpoint string    `yaml:"tokenEndpoint"`
	AccessToken   string    `yaml:"accessToken"`
	RefreshToken  string    `yaml:"refreshToken,omitempty"`
	TokenType     string    `yaml:"tokenType,omitempty"`
	Expiry        time.Time `yaml:"expiry,omitempty"`
}

// Store is credentials.yaml.
type Store struct {
	// Clients are keyed by the provider's issuer.
	Clients map[string]Client `yaml:"clients"`
	// Sessions are keyed by profile name.
	Sessions map[string]Session `yaml:"sessions"`
}

// Path is where credentials.yaml lives inside home.
func Path(home string) string {
	return filepath.Join(home, "credentials.yaml")
}

// Load reads credentials.yaml from home. No file is an empty store: nobody has
// signed in yet.
func Load(home string) (*Store, error) {

	ret := &Store{Clients: map[string]Client{}, Sessions: map[string]Session{}}
	data, err := os.ReadFile(Path(home))
	if errors.Is(err, os.ErrNotExist) {
		return ret, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", Path(home), err)
	}
	if err := yaml.Unmarshal(data, ret); err != nil {
		return nil, fmt.Errorf("%s is not valid YAML (delete it and sign in again): %w", Path(home), err)
	}
	if ret.Clients == nil {
		ret.Clients = map[string]Client{}
	}
	if ret.Sessions == nil {
		ret.Sessions = map[string]Session{}
	}
	return ret, nil
}

// Save writes s to home, readable by you alone.
func (s *Store) Save(home string) error {

	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", home, err)
	}
	body, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("render credentials: %w", err)
	}
	data := append([]byte("# Written by `octopaas auth login`. Do not share this file.\n"), body...)

	//we write a sibling and rename it over, so a crash mid-save cannot cost you
	//your session, and the file is never readable by anyone else even briefly.
	tmp, err := os.CreateTemp(home, ".credentials-*")
	if err != nil {
		return fmt.Errorf("save %s: %w", Path(home), err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save %s: %w", Path(home), err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save %s: %w", Path(home), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save %s: %w", Path(home), err)
	}
	return os.Rename(tmp.Name(), Path(home))
}
