package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/config"
)

func (a *App) environmentsCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "environments",
		Aliases: []string{"envs"},
		Short:   "List environments, and choose the one your commands act in",
		Long: `Environments are isolated slices of a platform, each with its own
integrations, deployments and secrets. Every command acts in one: --environment
when given, then $OCTOPAAS_ENVIRONMENT, then the profile's, then octo, the
default.`,
	}
	cmd.AddCommand(a.environmentsListCommand(), a.environmentsUseCommand(), a.environmentsCurrentCommand())
	return cmd
}

// environmentRow is one environment as `environments list` prints it.
type environmentRow struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Namespace   string `json:"namespace"`
	Current     bool   `json:"current"`
}

func (a *App) environmentsListCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the platform's environments",
		Example: `  octopaas environments list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {

			api, err := a.client()
			if err != nil {
				return err
			}
			environments, err := api.ListEnvironments(cmd.Context())
			if err != nil {
				return err
			}
			current := config.DisplayEnvironment(a.resolved.Environment)
			rows := make([]environmentRow, 0, len(environments))
			for _, e := range environments {
				rows = append(rows, environmentRow{
					Name: e.Name, DisplayName: e.DisplayName, Namespace: e.Namespace, Current: e.Name == current,
				})
			}
			return a.emit(rows, func() {
				table := make([][]string, 0, len(rows))
				for _, row := range rows {
					mark := ""
					if row.Current {
						mark = "● in effect"
					}
					table = append(table, []string{row.Name, row.DisplayName, row.Namespace, mark})
				}
				a.ui.Table([]string{"ENVIRONMENT", "NAME", "NAMESPACE", ""}, table)
			})
		},
	}
}

func (a *App) environmentsUseCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "use <environment>",
		Short: "Make the current profile act in an environment",
		Long: `Make the profile in effect act in an environment from now on, when neither
--environment nor $OCTOPAAS_ENVIRONMENT says otherwise. "octo" is the default.

The name is checked against the platform first, so a typo is an error now
rather than a refusal on every later command.`,
		Example: `  octopaas environments use staging
  octopaas environments use octo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			name := config.NormalizeEnvironment(args[0])
			r, err := a.resolve()
			if err != nil {
				return err
			}
			p, ok := a.cfg.Profiles[r.Profile]
			if !ok {
				return usageError{fmt.Errorf("profile %q is not in your config, so there is nothing to save the environment on", r.Profile)}
			}
			if name != "" {
				api, err := a.client()
				if err != nil {
					return err
				}
				environments, err := api.ListEnvironments(cmd.Context())
				if err != nil {
					return err
				}
				found := false
				for _, e := range environments {
					found = found || e.Name == name
				}
				if !found {
					return usageError{fmt.Errorf("there is no environment %q on %s; see `octopaas environments list`", name, r.URL)}
				}
			}
			//the default is saved by its name rather than as nothing: an empty one names
			//no environment, and a key bound to another would quietly act in its own.
			p.Environment = config.DisplayEnvironment(name)
			a.cfg.Profiles[r.Profile] = p
			if err := a.cfg.Save(a.home); err != nil {
				return err
			}
			a.ui.Success("Profile %s now acts in %s", a.ui.Bold(r.Profile), a.ui.Bold(config.DisplayEnvironment(name)))
			if env := a.Getenv(config.EnvEnvironment); env != "" && config.NormalizeEnvironment(env) != name {
				a.ui.Warn("OCTOPAAS_ENVIRONMENT=%s is set in this shell and still wins here.", env)
			}
			return nil
		},
	}
}

// currentEnvironment is `environments current` for machines.
type currentEnvironment struct {
	Environment string `json:"environment"`
	Source      string `json:"source"`
	Profile     string `json:"profile"`
}

func (a *App) environmentsCurrentCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "current",
		Short:   "Show the environment commands act in, and why",
		Example: `  octopaas environments current`,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {

			r, err := a.resolve()
			if err != nil {
				return err
			}
			report := currentEnvironment{
				Environment: config.DisplayEnvironment(r.Environment),
				Source:      string(r.EnvironmentSource),
				Profile:     r.Profile,
			}
			return a.emit(report, func() {
				a.ui.Print("%s %s", report.Environment, a.ui.Dim("("+report.Source+")"))
			})
		},
	}
}
