package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"time"

	"golang.org/x/oauth2"

	"github.com/juancavallotti/octo/octopaas/internal/credentials"
)

// CallbackPath is where the provider sends the browser back to.
const CallbackPath = "/callback"

// PreferredPort is the loopback port octopaas listens on first. A fixed port lets
// one registration serve every sign-in; when it is taken we listen anywhere and
// register again for that port.
const PreferredPort = 8976

// Listen opens the loopback listener the browser comes back to, and says which
// redirect URI it answers.
func Listen() (net.Listener, string, error) {

	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", PreferredPort))
	if err != nil {
		if l, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return nil, "", fmt.Errorf("open a local port for the browser to come back to: %w", err)
		}
	}
	return l, fmt.Sprintf("http://%s%s", l.Addr().String(), CallbackPath), nil
}

// Login is one browser sign-in.
type Login struct {
	HTTP     *http.Client
	Metadata Metadata
	Client   credentials.Client
	Listener net.Listener
	// Open shows the person the sign-in page. It is told the URL whatever
	// happens, so it can also print it for a machine with no browser.
	Open func(url string) error
	// Page is the HTML the browser is left on, given whether it worked.
	Page func(ok bool, message string) string
}

// Run sends the person to sign in and waits for the provider to send them back,
// then trades the code for tokens. It gives up when ctx does.
func (l *Login) Run(ctx context.Context) (*oauth2.Token, error) {

	conf := l.config()
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()
	authURL := conf.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("resource", l.Metadata.Resource),
	)

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, func(w http.ResponseWriter, r *http.Request) {

		q := r.URL.Query()
		var res result
		switch {
		case q.Get("state") != state:
			//not our request: a stale tab, or somebody else's redirect. We say so
			//and keep waiting for the real one.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(l.Page(false, "This sign-in link is stale. Start again from your terminal.")))
			return
		case q.Get("error") != "":
			res.err = fmt.Errorf("the identity provider refused the sign-in: %s %s", q.Get("error"), q.Get("error_description"))
		case q.Get("code") == "":
			res.err = errors.New("the identity provider came back without a code")
		default:
			res.code = q.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.err != nil {
			_, _ = w.Write([]byte(l.Page(false, res.err.Error())))
		} else {
			_, _ = w.Write([]byte(l.Page(true, "You are signed in. You can close this tab and go back to your terminal.")))
		}
		select {
		case done <- res:
		default:
		}
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(l.Listener) }()
	defer func() { _ = server.Close() }()

	if err := l.Open(authURL); err != nil {
		return nil, err
	}

	var res result
	select {
	case res = <-done:
	case <-ctx.Done():
		return nil, fmt.Errorf("gave up waiting for the browser: %w", ctx.Err())
	}
	if res.err != nil {
		return nil, res.err
	}

	exchangeCtx := context.WithValue(ctx, oauth2.HTTPClient, l.HTTP)
	token, err := conf.Exchange(exchangeCtx, res.code,
		oauth2.VerifierOption(verifier),
		oauth2.SetAuthURLParam("resource", l.Metadata.Resource),
	)
	if err != nil {
		return nil, fmt.Errorf("trade the sign-in code for a token: %w", err)
	}
	return token, nil
}

// Scopes are what octopaas asks the provider for: who you are, and a refresh
// token where the provider says it knows offline_access. Some refuse a scope
// they do not recognise, and others hand out refresh tokens regardless.
func Scopes(md Metadata) []string {

	ret := []string{"openid", "profile", "email"}
	if slices.Contains(md.ScopesSupported, "offline_access") {
		ret = append(ret, "offline_access")
	}
	return ret
}

func (l *Login) config() *oauth2.Config {

	return &oauth2.Config{
		ClientID:    l.Client.ClientID,
		RedirectURL: l.Client.RedirectURI,
		Scopes:      Scopes(l.Metadata),
		Endpoint: oauth2.Endpoint{
			AuthURL:   l.Metadata.AuthorizationEndpoint,
			TokenURL:  l.Metadata.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

func randomState() (string, error) {

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate a sign-in state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
