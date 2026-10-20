package cmd

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/config"
	"github.com/juancavallotti/octo/octopaas/internal/credentials"
	"github.com/juancavallotti/octo/octopaas/internal/oauth"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

// apiKey is a bearer that never changes: refreshing it is pointless, so a
// refused key fails as it is.
type apiKey string

func (k apiKey) Bearer(context.Context, bool) (string, error) { return string(k), nil }

// bearer is how the resolved profile signs its requests: an API key when there
// is one, its OAuth session otherwise.
func (a *App) bearer(r config.Resolved) client.Bearer {

	if r.APIKey != "" {
		return apiKey(r.APIKey)
	}
	return &oauth.Source{HTTP: a.httpClient(), Home: a.home, Profile: r.Profile}
}

// httpClient is for talking to the identity provider: it says who it is, since
// some providers' edges refuse a library's default user agent.
func (a *App) httpClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: userAgent{name: a.userAgent(), next: http.DefaultTransport}}
}

type userAgent struct {
	name string
	next http.RoundTripper
}

func (u userAgent) RoundTrip(r *http.Request) (*http.Response, error) {

	r = r.Clone(r.Context())
	r.Header.Set("User-Agent", u.name)
	return u.next.RoundTrip(r)
}

func (a *App) authCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Sign in, sign out, and see how you are signed in",
		Long: `Sign in to a platform with your browser, the way people should.

` + "`auth login`" + ` finds the platform's identity provider from the API itself,
registers octopaas with it, and sends you through its sign-in page. The session
is kept in ~/.octopaas/credentials.yaml, per profile, and renews itself.

API keys are for automation: see ` + "`octopaas help environment`" + `.`,
	}
	cmd.AddCommand(a.loginCommand(), a.logoutCommand(), a.statusCommand("status"), a.tokenCommand())
	return cmd
}

func (a *App) loginCommand() *cobra.Command {

	var noBrowser bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with your browser",
		Long: `Sign in with your browser, and keep the session in the profile.

octopaas asks the API which identity provider issues its tokens, registers
itself there once (dynamic client registration), and opens the sign-in page.
When you come back, the session is saved for the profile and renews itself
until you sign out.

On a machine without a browser, use --no-browser and open the printed link
anywhere that can reach this machine's localhost.`,
		Example: `  octopaas auth login
  octopaas auth login --profile local --url http://localhost:8095
  octopaas auth login --no-browser`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.login(cmd.Context(), noBrowser, timeout)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the sign-in link instead of opening it")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "how long to wait for you in the browser")
	return cmd
}

func (a *App) login(ctx context.Context, noBrowser bool, timeout time.Duration) error {

	r, err := a.resolve()
	if err != nil {
		return err
	}
	if r.KeySource == config.FromEnv {
		a.ui.Warn("OCTOPAAS_KEY is set, and it wins over any session: commands in this shell will keep using the key.")
	}

	//a --url given at sign-in is remembered, so the profile goes on talking to
	//the platform you signed in to.
	if a.url != "" {
		p := a.cfg.Profiles[r.Profile]
		p.URL = r.URL
		a.cfg.Profiles[r.Profile] = p
		if err := a.cfg.Save(a.home); err != nil {
			return err
		}
	}

	hc := a.httpClient()
	var md oauth.Metadata
	err = a.ui.Spin(ui.Tentacles, "Asking "+r.URL+" who signs you in", func(*ui.Live) error {
		md, err = oauth.Discover(ctx, hc, r.URL)
		return err
	})
	if err != nil {
		return err
	}

	store, err := credentials.Load(a.home)
	if err != nil {
		return err
	}
	listener, redirect, err := oauth.Listen()
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	registration, known := store.Clients[md.Issuer]
	//a registration only serves sign-ins that match it: the same way back, and
	//the same scopes. Anything else is registered afresh.
	if !known || registration.RedirectURI != redirect || registration.Scope != strings.Join(oauth.Scopes(md), " ") {
		err = a.ui.Spin(ui.Bubbles, "Introducing octopaas to "+md.Issuer, func(*ui.Live) error {
			registration, err = oauth.Register(ctx, hc, md, redirect)
			return err
		})
		if err != nil {
			return err
		}
		store.Clients[md.Issuer] = registration
		if err := store.Save(a.home); err != nil {
			return err
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	login := &oauth.Login{
		HTTP:     hc,
		Metadata: md,
		Client:   registration,
		Listener: listener,
		Page:     callbackPage,
		Open: func(link string) error {
			if noBrowser {
				a.ui.Box("Sign in to "+r.URL, "Open this link in a browser on this machine:", "", link)
				return nil
			}
			a.ui.Box("Sign in to "+r.URL, "Opening your browser. If it does not open, visit:", "", link)
			browser.Stdout, browser.Stderr = io.Discard, io.Discard
			if err := browser.OpenURL(link); err != nil {
				a.ui.Hint("Could not open a browser (%v); open the link yourself.", err)
			}
			return nil
		},
	}

	var token *oauthToken
	err = a.ui.Spin(ui.Tentacles, "Waiting for you in the browser", func(*ui.Live) error {
		t, err := login.Run(waitCtx)
		if err == nil {
			token = &oauthToken{access: t.AccessToken, refresh: t.RefreshToken, kind: t.TokenType, expiry: t.Expiry}
		}
		return err
	})
	if err != nil {
		return err
	}

	store.Sessions[r.Profile] = credentials.Session{
		Issuer:        md.Issuer,
		Resource:      md.Resource,
		ClientID:      registration.ClientID,
		TokenEndpoint: md.TokenEndpoint,
		AccessToken:   token.access,
		RefreshToken:  token.refresh,
		TokenType:     token.kind,
		Expiry:        token.expiry.UTC(),
	}
	if err := store.Save(a.home); err != nil {
		return err
	}

	//we prove the session works before saying so: a provider can hand out a
	//token the platform has never heard of.
	api := client.New(r.URL, &oauth.Source{HTTP: hc, Home: a.home, Profile: r.Profile}, a.userAgent())
	me, err := api.Me(ctx)
	if err != nil {
		return err
	}
	a.ui.Say("")
	a.ui.Banner("")
	a.ui.Moment(ui.Happy, fmt.Sprintf("Welcome aboard, %s!", nameOf(me)),
		fmt.Sprintf("Signed in to %s as %s, on profile %s.", r.URL, me.Email, r.Profile))
	return nil
}

type oauthToken struct {
	access, refresh, kind string
	expiry                time.Time
}

func (a *App) logoutCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the profile's session",
		Long: `Forget the profile's session on this machine.

The identity provider's own session in your browser is untouched; sign out
there too if this is a shared machine.`,
		Example: `  octopaas auth logout
  octopaas auth logout --profile local`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {

			r, err := a.resolve()
			if err != nil {
				return err
			}
			store, err := credentials.Load(a.home)
			if err != nil {
				return err
			}
			if _, ok := store.Sessions[r.Profile]; !ok {
				a.ui.Say("Profile %s was not signed in.", r.Profile)
				return nil
			}
			delete(store.Sessions, r.Profile)
			if err := store.Save(a.home); err != nil {
				return err
			}
			a.ui.Moment(ui.Sleepy, "Signed out of profile "+r.Profile+".", "See you soon.")
			return nil
		},
	}
}

// statusReport is `auth status` for machines.
type statusReport struct {
	Profile       string       `json:"profile"`
	ProfileSource string       `json:"profileSource"`
	URL           string       `json:"url"`
	URLSource     string       `json:"urlSource"`
	Credential    string       `json:"credential"`
	Source        string       `json:"credentialSource,omitempty"`
	Expiry        *time.Time   `json:"expiresAt,omitempty"`
	User          *client.User `json:"user,omitempty"`
}

func (a *App) whoamiCommand() *cobra.Command {

	cmd := a.statusCommand("whoami")
	cmd.Short = "Who you are signed in as, and where"
	return cmd
}

func (a *App) statusCommand(use string) *cobra.Command {

	return &cobra.Command{
		Use:   use,
		Short: "Show who you are signed in as, and with what",
		Long: `Show the platform, the profile and the credential in use, and the account
it acts as, with your roles.

It says where each came from (a flag, the environment or the config), which is
the quickest way to find out why octopaas is talking to the platform it is.`,
		Example: `  octopaas ` + use + `
  octopaas ` + use + ` -o json | jq -r .user.email
  OCTOPAAS_PROFILE=local octopaas ` + use,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.status(cmd.Context())
		},
	}
}

func (a *App) status(ctx context.Context) error {

	r, err := a.resolve()
	if err != nil {
		return err
	}
	report := statusReport{Profile: r.Profile, ProfileSource: string(r.ProfileSource), URL: r.URL, URLSource: string(r.URLSource)}

	if r.APIKey != "" {
		report.Credential = "apiKey"
		report.Source = string(r.KeySource)
	} else {
		store, err := credentials.Load(a.home)
		if err != nil {
			return err
		}
		if s, ok := store.Sessions[r.Profile]; ok {
			report.Credential = "oauth"
			report.Source = s.Issuer
			if !s.Expiry.IsZero() {
				report.Expiry = &s.Expiry
			}
		} else {
			report.Credential = "none"
		}
	}

	api, err := a.client()
	if err != nil {
		return err
	}
	if report.Credential != "none" {
		var me client.User
		err := a.ui.Spin(ui.Tentacles, "Asking "+r.URL+" who you are", func(*ui.Live) error {
			me, err = api.Me(ctx)
			return err
		})
		if err != nil {
			return err
		}
		report.User = &me
	}

	return a.emit(report, func() {

		credential := "none — run `octopaas auth login`"
		switch report.Credential {
		case "apiKey":
			credential = fmt.Sprintf("API key %s (from %s)", config.MaskKey(r.APIKey), keySourceName(r.KeySource))
		case "oauth":
			credential = "signed in with " + report.Source
			if report.Expiry != nil {
				credential += fmt.Sprintf(", token renews %s", until(*report.Expiry))
			}
		}
		pairs := [][2]string{
			{"Profile", fmt.Sprintf("%s %s", r.Profile, a.ui.Dim("("+string(r.ProfileSource)+")"))},
			{"Platform", fmt.Sprintf("%s %s", r.URL, a.ui.Dim("("+string(r.URLSource)+")"))},
			{"Credential", credential},
		}
		if report.User != nil {
			pairs = append(pairs,
				[2]string{"Name", nameOf(*report.User)},
				[2]string{"Email", report.User.Email},
				[2]string{"Roles", strings.Join(report.User.Roles, ", ")},
				[2]string{"User id", report.User.ID},
			)
		}
		a.ui.Fields(pairs)
	})
}

func (a *App) tokenCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "token",
		Short: "Print the bearer token octopaas would send",
		Long: `Print the bearer token octopaas would send, renewed first if it has run out,
for calling the API with another tool. Treat it like a password.`,
		Example: `  curl -H "Authorization: Bearer $(octopaas auth token)" https://api.octopaas.dev/v1/me`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {

			r, err := a.resolve()
			if err != nil {
				return err
			}
			token, err := a.bearer(r).Bearer(cmd.Context(), false)
			if err != nil {
				return err
			}
			a.ui.Print("%s", token)
			return nil
		},
	}
}

func keySourceName(s config.Source) string {

	if s == config.FromEnv {
		return config.EnvKey
	}
	return "the profile"
}

func nameOf(u client.User) string {

	if u.Name != "" {
		return u.Name
	}
	if u.Email != "" {
		return u.Email
	}
	return u.ID
}

func until(t time.Time) string {

	d := time.Until(t).Round(time.Minute)
	if d <= 0 {
		return "on the next call"
	}
	return "in " + strings.TrimSuffix(d.String(), "0s")
}

// callbackPage is where the browser lands after signing in: the octopus, and
// what to do next.
func callbackPage(ok bool, message string) string {

	mood, color := ui.Happy, "#2B8A3E"
	if !ok {
		mood, color = ui.Confused, "#C92A2A"
	}
	art := html.EscapeString(strings.Join(ui.Critter(mood), "\n"))
	return `<!doctype html><html><head><meta charset="utf-8"><title>octopaas</title>
<style>body{font-family:ui-monospace,Menlo,monospace;background:#1a1b26;color:#e6e6e6;display:flex;
align-items:center;justify-content:center;height:100vh;margin:0}main{text-align:center}
pre{color:` + color + `;font-size:22px;line-height:1.2}p{font-size:16px;max-width:36em}</style></head>
<body><main><pre>` + art + `</pre><p>` + html.EscapeString(message) + `</p></main></body></html>`
}
