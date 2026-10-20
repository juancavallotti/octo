package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/octopaas/internal/credentials"
)

// ErrSignedOut is a profile with no usable session.
var ErrSignedOut = errors.New("you are not signed in")

// earlyRefresh is how long before expiry a token is treated as spent, so a
// request never sets off with a token that dies on the way.
const earlyRefresh = 30 * time.Second

// Source is a signed-in profile's bearer: it hands out the access token, and
// refreshes and saves it when it runs out.
type Source struct {
	HTTP    *http.Client
	Home    string
	Profile string
	// URL is the API the token is about to be sent to. A session signed in to
	// any other is refused, so a --url or OCTOPAAS_URL pointing elsewhere can
	// never carry this platform's token to another host.
	URL string

	mu      sync.Mutex
	session credentials.Session
	loaded  bool
}

// Bearer is the profile's access token, refreshed first if it has expired or
// the API just refused it.
func (s *Source) Bearer(ctx context.Context, refresh bool) (string, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.loaded {
		store, err := credentials.Load(s.Home)
		if err != nil {
			return "", err
		}
		session, ok := store.Sessions[s.Profile]
		if !ok || session.AccessToken == "" {
			return "", ErrSignedOut
		}
		if strings.TrimRight(session.URL, "/") != strings.TrimRight(s.URL, "/") {
			return "", fmt.Errorf("%w to %s: this profile's session is for %s, and is never sent anywhere else",
				ErrSignedOut, s.URL, firstNonEmpty(session.URL, "another platform"))
		}
		s.session, s.loaded = session, true
	}

	fresh := s.session.Expiry.IsZero() || time.Until(s.session.Expiry) > earlyRefresh
	if fresh && !refresh {
		return s.session.AccessToken, nil
	}
	if s.session.RefreshToken == "" {
		return "", fmt.Errorf("%w: your session expired and cannot be renewed", ErrSignedOut)
	}

	renewed, err := Refresh(ctx, s.HTTP, s.session)
	if err != nil {
		return "", fmt.Errorf("%w: renewing your session failed: %v", ErrSignedOut, err)
	}
	s.session = renewed

	//we save the renewed session straight away: a provider that rotates
	//refresh tokens has just made the old one worthless.
	store, err := credentials.Load(s.Home)
	if err != nil {
		return "", err
	}
	store.Sessions[s.Profile] = renewed
	if err := store.Save(s.Home); err != nil {
		return "", err
	}
	return renewed.AccessToken, nil
}

// Refresh trades a session's refresh token for a new access token. It is written
// out rather than left to x/oauth2 because the request must name the API as its
// resource (RFC 8707) again, or the provider is free to mint a token for some
// other audience that the API would refuse.
func Refresh(ctx context.Context, hc *http.Client, session credentials.Session) (credentials.Session, error) {

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {session.RefreshToken},
		"client_id":     {session.ClientID},
		"resource":      {session.Resource},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, session.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return session, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return session, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return session, fmt.Errorf("%s answered %d: %s", session.TokenEndpoint, resp.StatusCode, oauthError(body))
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return session, fmt.Errorf("%s sent back no access token", session.TokenEndpoint)
	}

	ret := session
	ret.AccessToken = token.AccessToken
	ret.TokenType = token.TokenType
	//a provider that does not rotate sends no new refresh token, and the old one
	//is still good.
	if token.RefreshToken != "" {
		ret.RefreshToken = token.RefreshToken
	}
	ret.Expiry = time.Time{}
	if token.ExpiresIn > 0 {
		ret.Expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC()
	}
	return ret, nil
}

func firstNonEmpty(values ...string) string {

	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
