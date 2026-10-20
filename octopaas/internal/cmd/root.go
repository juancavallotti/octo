// Package cmd is octopaas's command tree. Each file holds one group of
// commands, built against the App that every command shares: the resolved
// profile, the API client, and the UI.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/config"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

// App is what every command runs with.
type App struct {
	Version   string
	BuildDate string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string

	// flags every command takes
	profile string
	url     string
	output  string
	plain   bool

	ui       *ui.UI
	home     string
	cfg      *config.File
	resolved config.Resolved
	api      *client.Client
}

// Output formats for -o.
const (
	outputTable = "table"
	outputJSON  = "json"
	outputYAML  = "yaml"
)

// Execute runs octopaas with args and returns its exit status.
func Execute(ctx context.Context, app *App, args []string) int {

	root := NewRoot(app)
	root.SetArgs(args)
	root.SetIn(app.Stdin)
	root.SetOut(app.Stdout)
	root.SetErr(app.Stderr)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return 130
	}
	if app.ui == nil {
		app.ui = ui.New(app.Stdout, app.Stderr, ui.Options{Plain: true, Getenv: app.Getenv})
	}
	app.explain(err)
	var usage usageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

// NewRoot builds the whole command tree. The docs generator walks the same tree,
// so what `--help` says and what the reference pages say cannot drift.
func NewRoot(app *App) *cobra.Command {

	root := &cobra.Command{
		Use:   "octopaas",
		Short: "Manage, deploy and watch your Octo integrations from the terminal",
		Long: `octopaas is the command line for an Octo platform's public API.

It edits integrations and their files, freezes them into versions, deploys
those versions and follows what they log, all as you: your roles decide what
it may do, and every change is attributed to you.

Sign in once with ` + "`octopaas auth login`" + `. For automation, set OCTOPAAS_URL and
OCTOPAAS_KEY instead and no sign-in or config file is needed.`,
		Example: `  # sign in, and check who you are
  octopaas auth login
  octopaas whoami

  # pull an integration, edit it, push it back and ship it
  octopaas integrations pull billing
  octopaas integrations push billing ./billing
  octopaas versions create billing v1.3.0
  octopaas deployments rollout billing-prod --version v1.3.0`,
		Version:       app.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return app.setup(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			app.ui.Banner("your reef, from the terminal")
			return cmd.Help()
		},
	}
	root.SetVersionTemplate(`{{.Version}}` + "\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })

	flags := root.PersistentFlags()
	flags.StringVarP(&app.profile, "profile", "p", "", "the profile to use (default: $OCTOPAAS_PROFILE, then the config's current one)")
	flags.StringVar(&app.url, "url", "", "the API to talk to, overriding the profile (default: $OCTOPAAS_URL)")
	flags.StringVarP(&app.output, "output", "o", outputTable, "how to print results: table, json or yaml")
	flags.BoolVar(&app.plain, "plain", false, "no colours, animations or art, even in a terminal")

	root.AddGroup(
		&cobra.Group{ID: "account", Title: "Your account and setup:"},
		&cobra.Group{ID: "build", Title: "Building integrations:"},
		&cobra.Group{ID: "run", Title: "Shipping and running them:"},
		&cobra.Group{ID: "more", Title: "More:"},
	)
	root.SetHelpCommandGroupID("more")
	root.SetCompletionCommandGroupID("more")

	for _, c := range []*cobra.Command{app.authCommand(), app.whoamiCommand(), app.profileCommand(), app.configCommand()} {
		c.GroupID = "account"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{app.integrationsCommand(), app.filesCommand(), app.resourcesCommand(), app.versionsCommand()} {
		c.GroupID = "build"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{app.deploymentsCommand(), app.logsCommand()} {
		c.GroupID = "run"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{app.apiCommand(), app.versionCommand()} {
		c.GroupID = "more"
		root.AddCommand(c)
	}
	root.AddCommand(environmentTopic())
	return root
}

// setup runs before every command: it dresses the UI and finds the config,
// writing a fresh one on the very first run.
func (a *App) setup(cmd *cobra.Command) error {

	switch a.output {
	case outputTable, outputJSON, outputYAML:
	default:
		return usageError{fmt.Errorf("--output must be table, json or yaml, not %q", a.output)}
	}
	a.ui = ui.New(a.Stdout, a.Stderr, ui.Options{Plain: a.plain, Machine: a.output != outputTable, Getenv: a.Getenv})

	home, err := config.Home(a.Getenv)
	if err != nil {
		return err
	}
	a.home = home

	//a CI job driven by the environment alone gets no file written for it: its
	//home may be read-only, and nothing would ever read the file back.
	if a.Getenv(config.EnvURL) != "" && a.Getenv(config.EnvKey) != "" {
		if _, statErr := os.Stat(config.Path(home)); errors.Is(statErr, os.ErrNotExist) {
			a.cfg = config.Fresh()
			return nil
		}
	}

	cfg, created, err := config.Load(home)
	if err != nil {
		return err
	}
	a.cfg = cfg
	if created && cmd.Name() != "__complete" {
		a.welcome()
	}
	return nil
}

// welcome greets a first run, once.
func (a *App) welcome() {

	a.ui.Banner("hello! nice to meet you")
	a.ui.Success("Created %s", config.Path(a.home))
	a.ui.Hint("It points the default profile at %s. Sign in with `octopaas auth login`.", config.DefaultURL)
	a.ui.Say("")
}

// resolve settles which profile, URL and credential this command uses.
func (a *App) resolve() (config.Resolved, error) {

	r, err := config.Resolve(a.cfg, config.Overrides{Profile: a.profile, URL: a.url}, a.Getenv)
	if err != nil {
		return r, usageError{err}
	}
	a.resolved = r
	return r, nil
}

// client is the API client for this command, authenticated with whatever the
// resolved profile signs in with.
func (a *App) client() (*client.Client, error) {

	if a.api != nil {
		return a.api, nil
	}
	r, err := a.resolve()
	if err != nil {
		return nil, err
	}
	a.api = client.New(r.URL, a.bearer(r), a.userAgent())
	return a.api, nil
}

func (a *App) userAgent() string {
	return "octopaas/" + a.Version
}

// usageError is a command used wrongly: exit status 2, like every other CLI.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }
func (u usageError) Unwrap() error { return u.err }
