// Package oauth signs a person in to the public API the standard way, with no
// knowledge of which identity provider an installation uses:
//
//  1. Discover asks the API who issues its tokens (RFC 9728), then asks that
//     provider how to talk to it (RFC 8414 / OpenID discovery).
//  2. Register introduces octopaas to the provider as a public client (RFC 7591),
//     once per provider.
//  3. Login opens the browser on an authorization-code request with PKCE (RFC
//  7636. for the API as the resource (RFC 8707), and catches the answer on a
//     loopback port (RFC 8252).
//  4. Source keeps the session fresh afterwards, refreshing the token before it
//     runs out.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Metadata is everything sign-in needs to know, gathered from the API and its
// provider.
type Metadata struct {
	// Resource is what tokens must be issued for: the API's own identifier.
	Resource string
	Issuer   string

	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	ScopesSupported       []string
}

// ErrNoRegistration is a provider that does not let clients register themselves.
var ErrNoRegistration = errors.New("the identity provider does not support dynamic client registration, so octopaas cannot sign you in with it; use an API key (OCTOPAAS_KEY) instead")

// Discover learns, from the API at apiURL alone, which provider issues its
// tokens and where that provider's endpoints are.
func Discover(ctx context.Context, hc *http.Client, apiURL string) (Metadata, error) {

	var ret Metadata
	apiURL = strings.TrimRight(apiURL, "/")

	//the standard way in is to be refused: the API's 401 says where its
	//metadata is. We fall back on the well-known path for an API that does not.
	metadataURL := challengedMetadata(ctx, hc, apiURL)
	if metadataURL == "" {
		metadataURL = apiURL + "/.well-known/oauth-protected-resource"
	}

	var resource struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := getJSON(ctx, hc, metadataURL, &resource); err != nil {
		return ret, fmt.Errorf("ask %s how to sign in: %w", apiURL, err)
	}
	if len(resource.AuthorizationServers) == 0 {
		return ret, fmt.Errorf("%s names no identity provider, so it cannot be signed in to with OAuth; use an API key instead", apiURL)
	}
	ret.Resource = resource.Resource
	if ret.Resource == "" {
		ret.Resource = apiURL
	}
	ret.Issuer = strings.TrimRight(resource.AuthorizationServers[0], "/")

	//we read the provider's own document rather than the copy the API relays:
	//it is the provider's word on its endpoints, and its issuer can be checked.
	var provider struct {
		Issuer                string   `json:"issuer"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		RegistrationEndpoint  string   `json:"registration_endpoint"`
		ScopesSupported       []string `json:"scopes_supported"`
	}
	var lastErr error
	for _, doc := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		if lastErr = getJSON(ctx, hc, ret.Issuer+doc, &provider); lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return ret, fmt.Errorf("read %s's discovery document: %w", ret.Issuer, lastErr)
	}
	if strings.TrimRight(provider.Issuer, "/") != ret.Issuer {
		return ret, fmt.Errorf("%s describes itself as issuer %q; refusing to sign in with a provider that is not who the API says it is", ret.Issuer, provider.Issuer)
	}
	if provider.AuthorizationEndpoint == "" || provider.TokenEndpoint == "" {
		return ret, fmt.Errorf("%s does not publish an authorization and a token endpoint", ret.Issuer)
	}
	if provider.RegistrationEndpoint == "" {
		return ret, ErrNoRegistration
	}

	ret.AuthorizationEndpoint = provider.AuthorizationEndpoint
	ret.TokenEndpoint = provider.TokenEndpoint
	ret.RegistrationEndpoint = provider.RegistrationEndpoint
	ret.ScopesSupported = provider.ScopesSupported
	return ret, nil
}

// challengedMetadata calls the API without a credential and reads the
// resource_metadata its 401 points at, or "" when it does not point anywhere.
func challengedMetadata(ctx context.Context, hc *http.Client, apiURL string) string {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/v1/me", nil)
	if err != nil {
		return ""
	}
	resp, err := hc.Do(req)
	if err != nil {
		return ""
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		return ""
	}
	return ResourceMetadataFrom(resp.Header.Get("WWW-Authenticate"))
}

// ResourceMetadataFrom pulls resource_metadata="..." out of a Bearer challenge.
func ResourceMetadataFrom(challenge string) string {

	const key = `resource_metadata="`
	_, rest, found := strings.Cut(challenge, key)
	if !found {
		return ""
	}
	value, _, _ := strings.Cut(rest, `"`)
	return value
}

func getJSON(ctx context.Context, hc *http.Client, url string, out any) error {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s is not JSON: %w", url, err)
	}
	return nil
}
