package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/juancavallotti/octo/octopaas/internal/config"
	"github.com/juancavallotti/octo/octopaas/internal/credentials"
)

func (a *App) profileCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "profile",
		Aliases: []string{"profiles"},
		Short:   "Switch between the platforms you work with",
		Long: `A profile is one platform: its API address, and optionally an API key.
You might have one for production and one for a local cluster.

Which one a command uses is, strongest first: --profile, then
OCTOPAAS_PROFILE, then the one ` + "`profile use`" + ` made current.`,
	}
	cmd.AddCommand(a.profileListCommand(), a.profileUseCommand(), a.profileAddCommand(), a.profileRemoveCommand())
	return cmd
}

type profileRow struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Current  bool   `json:"current"`
	InEffect bool   `json:"inEffect"`
	Auth     string `json:"auth"`
}

func (a *App) profileListCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List your profiles",
		Long: `List your profiles: which one is current, which one is in effect right now and
why, and how each signs in.`,
		Example: `  octopaas profile list
  OCTOPAAS_PROFILE=local octopaas profile list`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {

			//an unknown OCTOPAAS_PROFILE should not stop you listing the ones
			//you do have, so a failed resolve only means nothing is in effect.
			r, _ := config.Resolve(a.cfg, config.Overrides{Profile: a.profile, URL: a.url}, a.Getenv)
			store, err := credentials.Load(a.home)
			if err != nil {
				return err
			}

			var rows []profileRow
			for _, name := range a.cfg.Names() {
				p := a.cfg.Profiles[name]
				auth := "not signed in"
				if p.APIKey != "" {
					auth = "API key " + config.MaskKey(p.APIKey)
				} else if s, ok := store.Sessions[name]; ok {
					auth = "signed in with " + s.Issuer
				}
				rows = append(rows, profileRow{Name: name, URL: p.URL, Current: name == a.cfg.Current, InEffect: name == r.Profile, Auth: auth})
			}

			return a.emit(rows, func() {
				table := make([][]string, 0, len(rows))
				for _, row := range rows {
					mark := ""
					switch {
					case row.InEffect && row.Current:
						mark = "● current"
					case row.InEffect:
						mark = "● in effect (" + string(r.ProfileSource) + ")"
					case row.Current:
						mark = "○ current"
					}
					table = append(table, []string{row.Name, row.URL, row.Auth, mark})
				}
				a.ui.Table([]string{"PROFILE", "URL", "AUTH", ""}, table)
				if r.KeySource == config.FromEnv || r.URLSource == config.FromEnv {
					a.ui.Hint("OCTOPAAS_URL / OCTOPAAS_KEY are set and override the profile in effect.")
				}
			})
		},
	}
}

func (a *App) profileUseCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "use <profile>",
		Short: "Make a profile the current one",
		Long: `Make a profile the one commands use when neither --profile nor
OCTOPAAS_PROFILE says otherwise.`,
		Example:           `  octopaas profile use local`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: func(_ *cobra.Command, args []string) error {

			if _, ok := a.cfg.Profiles[args[0]]; !ok {
				return usageError{fmt.Errorf("there is no profile %q; add it with `octopaas profile add %s --url ...`", args[0], args[0])}
			}
			a.cfg.Current = args[0]
			if err := a.cfg.Save(a.home); err != nil {
				return err
			}
			a.ui.Success("Now using profile %s (%s)", a.ui.Bold(args[0]), a.cfg.Profiles[args[0]].URL)
			if env := a.Getenv(config.EnvProfile); env != "" && env != args[0] {
				a.ui.Warn("OCTOPAAS_PROFILE=%s is set in this shell and still wins here.", env)
			}
			return nil
		},
	}
}

func (a *App) profileAddCommand() *cobra.Command {

	var url string
	var withKey, use bool
	cmd := &cobra.Command{
		Use:   "add <profile>",
		Short: "Add a profile, or change one",
		Long: `Add a profile, or change the address or key of one you have.

People should sign in with ` + "`octopaas auth login --profile <name>`" + ` afterwards.
--with-key stores an API key in the profile instead, for a machine that runs
octopaas unattended; it is read from stdin, or asked for without echoing, so it
never lands in your shell history.`,
		Example: `  octopaas profile add local --url http://localhost:8095 --use
  octopaas auth login --profile local

  # an unattended machine
  echo "$KEY" | octopaas profile add builder --url https://api.octopaas.dev --with-key`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {

			name := args[0]
			p, existed := a.cfg.Profiles[name]
			if url != "" {
				p.URL = strings.TrimRight(url, "/")
			}
			if p.URL == "" {
				return usageError{fmt.Errorf("a new profile needs --url")}
			}
			if withKey {
				key, err := a.readSecret("API key for " + name)
				if err != nil {
					return err
				}
				p.APIKey = key
			}
			a.cfg.Profiles[name] = p
			if use || len(a.cfg.Profiles) == 1 {
				a.cfg.Current = name
			}
			if err := a.cfg.Save(a.home); err != nil {
				return err
			}

			verb := "Added"
			if existed {
				verb = "Updated"
			}
			a.ui.Success("%s profile %s → %s", verb, a.ui.Bold(name), p.URL)
			if p.APIKey == "" {
				a.ui.Hint("Sign in with `octopaas auth login --profile %s`.", name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "the platform's API address, such as https://api.octopaas.dev")
	cmd.Flags().BoolVar(&withKey, "with-key", false, "store an API key in the profile, read from stdin")
	cmd.Flags().BoolVar(&use, "use", false, "make it the current profile too")
	return cmd
}

func (a *App) profileRemoveCommand() *cobra.Command {

	return &cobra.Command{
		Use:               "rm <profile>",
		Aliases:           []string{"remove"},
		Short:             "Remove a profile and its session",
		Example:           `  octopaas profile rm staging`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: func(_ *cobra.Command, args []string) error {

			name := args[0]
			if _, ok := a.cfg.Profiles[name]; !ok {
				return usageError{fmt.Errorf("there is no profile %q", name)}
			}
			delete(a.cfg.Profiles, name)
			if a.cfg.Current == name {
				a.cfg.Current = ""
				if names := a.cfg.Names(); len(names) > 0 {
					a.cfg.Current = names[0]
				}
			}
			if err := a.cfg.Save(a.home); err != nil {
				return err
			}
			store, err := credentials.Load(a.home)
			if err != nil {
				return err
			}
			if _, ok := store.Sessions[name]; ok {
				delete(store.Sessions, name)
				if err := store.Save(a.home); err != nil {
					return err
				}
			}
			a.ui.Success("Removed profile %s", name)
			if a.cfg.Current != "" {
				a.ui.Hint("The current profile is now %s.", a.cfg.Current)
			}
			return nil
		},
	}
}

func (a *App) completeProfiles(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {

	if a.cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return a.cfg.Names(), cobra.ShellCompDirectiveNoFileComp
}

// readSecret reads one line from stdin, without echo when it is a terminal.
func (a *App) readSecret(prompt string) (string, error) {

	if f, ok := a.Stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		_, _ = fmt.Fprintf(a.Stderr, "%s %s: ", a.ui.Accent("?"), prompt)
		data, err := term.ReadPassword(int(f.Fd()))
		_, _ = fmt.Fprintln(a.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	line, err := bufio.NewReader(a.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", usageError{fmt.Errorf("expected the key on stdin")}
	}
	return strings.TrimSpace(line), nil
}

func (a *App) configCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Look at the config file",
		Long: `octopaas keeps its configuration in ~/.octopaas/config.yaml (or under
$OCTOPAAS_HOME), written on the first run. Sessions from ` + "`auth login`" + ` are kept
apart, in credentials.yaml beside it.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:     "path",
		Short:   "Print where the config file is",
		Example: `  $EDITOR "$(octopaas config path)"`,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			a.ui.Print("%s", config.Path(a.home))
			return nil
		},
	}, &cobra.Command{
		Use:     "view",
		Short:   "Print the configuration, with API keys masked",
		Example: `  octopaas config view -o json`,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {

			masked := &config.File{Current: a.cfg.Current, Profiles: map[string]config.Profile{}}
			for name, p := range a.cfg.Profiles {
				if p.APIKey != "" {
					p.APIKey = config.MaskKey(p.APIKey)
				}
				masked.Profiles[name] = p
			}
			if a.output == outputTable {
				a.output = outputYAML
			}
			return a.emit(masked, nil)
		},
	})
	return cmd
}
