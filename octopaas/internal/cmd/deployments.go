package cmd

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

func (a *App) deploymentsCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "deployments",
		Aliases: []string{"deployment", "deploy", "dep"},
		Short:   "Deploy versions, scale them, roll them out and take them down",
		Long: `A deployment runs one version of an integration on the platform. Commands take
a deployment by name or by id, and an integration by name or by id.

Deploying, scaling and rolling out wait until the deployment is running (or has
failed) and show you how it is going. Add --no-wait to return as soon as the
platform has accepted the change.`,
		Example: `  octopaas deployments create billing --version v1.3.0 --replicas 2
  octopaas deployments list
  octopaas deployments rollout billing --version v1.4.0`,
	}
	cmd.AddCommand(a.deploymentsListCommand(), a.deploymentsGetCommand(), a.deploymentsCreateCommand(),
		a.deploymentsOptionsCommand(), a.deploymentsScaleCommand(), a.deploymentsRolloutCommand(), a.deploymentsRemoveCommand())
	return cmd
}

func (a *App) deploymentsListCommand() *cobra.Command {

	var integration string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List deployments, newest first",
		Example: `  octopaas deployments list
  octopaas deployments list --integration billing`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var all []client.Deployment
			err = a.ui.Spin(ui.Tentacles, "Counting what is running", func(*ui.Live) error {
				if integration == "" {
					all, err = api.ListDeployments(ctx)
					return err
				}
				id, err := a.integration(ctx, api, integration)
				if err != nil {
					return err
				}
				all, err = api.ListIntegrationDeployments(ctx, id)
				return err
			})
			if err != nil {
				return err
			}
			return a.emit(all, func() {
				rows := make([][]string, 0, len(all))
				for _, d := range all {
					rows = append(rows, []string{d.Name, a.ui.Status(d.Status), d.Tag, replicas(d), d.ID, ago(d.CreatedAt)})
				}
				a.ui.Table([]string{"NAME", "STATUS", "VERSION", "READY", "ID", "CREATED"}, rows)
			})
		},
	}
	cmd.Flags().StringVarP(&integration, "integration", "i", "", "only this integration's deployments")
	return cmd
}

func (a *App) deploymentsGetCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "get <deployment>",
		Short:   "Show a deployment, its pods and where it answers",
		Example: `  octopaas deployments get billing`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var d client.Deployment
			err = a.ui.Spin(ui.Tentacles, "Checking on "+args[0], func(*ui.Live) error {
				id, err := a.deployment(ctx, api, args[0])
				if err != nil {
					return err
				}
				d, err = api.GetDeployment(ctx, id)
				return err
			})
			if err != nil {
				return err
			}
			return a.emit(d, func() { a.showDeployment(d) })
		},
	}
}

func (a *App) showDeployment(d client.Deployment) {

	pairs := [][2]string{
		{"Name", d.Name},
		{"Status", a.ui.Status(d.Status)},
		{"Version", d.Tag},
		{"Ready", replicas(d)},
		{"Id", d.ID},
	}
	if d.Reason != "" {
		pairs = append(pairs, [2]string{"Reason", d.Reason})
	}
	if d.InternalURL != "" {
		pairs = append(pairs, [2]string{"Internal", d.InternalURL})
	}
	if d.ExternalURL != "" {
		pairs = append(pairs, [2]string{"External", d.ExternalURL})
	}
	pairs = append(pairs, [2]string{"Created", ago(d.CreatedAt)})
	a.ui.Fields(pairs)

	if len(d.Pods) > 0 {
		a.ui.Print("")
		rows := make([][]string, 0, len(d.Pods))
		for _, p := range d.Pods {
			ready := "no"
			if p.Ready {
				ready = "yes"
			}
			rows = append(rows, []string{p.Name, a.ui.Status(strings.ToLower(p.Phase)), ready, fmt.Sprint(p.Restarts)})
		}
		a.ui.Table([]string{"POD", "PHASE", "READY", "RESTARTS"}, rows)
	}
}

// deployFlags are what create and rollout both say about a deployment.
type deployFlags struct {
	version string
	env     []string
	secrets []string
	tracing bool
	noWait  bool
	timeout time.Duration
	// replacing are the pods a rollout replaces: until they are gone, the
	// replicas that read as ready are still the old ones.
	replacing []string
}

func (f *deployFlags) bind(cmd *cobra.Command) {

	cmd.Flags().StringVar(&f.version, "version", "", "the version to run, by tag or id (create: default the integration as it stands)")
	cmd.Flags().StringArrayVar(&f.env, "env", nil, "bind a declared env var to a value, as NAME=value (repeatable)")
	cmd.Flags().StringArrayVar(&f.secrets, "secret", nil, "bind a declared env var to a cluster secret, as NAME=secret-name (repeatable)")
	cmd.Flags().BoolVar(&f.tracing, "tracing", false, "record traces for this deployment")
	cmd.Flags().BoolVar(&f.noWait, "no-wait", false, "return once the platform accepts the change")
	cmd.Flags().DurationVar(&f.timeout, "timeout", 5*time.Minute, "how long to wait for it to be running")
}

func (f *deployFlags) bindings() (map[string]client.EnvBinding, error) {

	if len(f.env)+len(f.secrets) == 0 {
		return nil, nil
	}
	ret := map[string]client.EnvBinding{}
	for _, kv := range f.env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			return nil, usageError{fmt.Errorf("--env wants NAME=value, got %q", kv)}
		}
		ret[name] = client.EnvBinding{Value: value}
	}
	for _, kv := range f.secrets {
		name, secret, ok := strings.Cut(kv, "=")
		if !ok || name == "" || secret == "" {
			return nil, usageError{fmt.Errorf("--secret wants NAME=secret-name, got %q", kv)}
		}
		ret[name] = client.EnvBinding{Secret: secret}
	}
	return ret, nil
}

func (a *App) deploymentsCreateCommand() *cobra.Command {

	var f deployFlags
	var replicaCount int
	var slug, expose, subdomain, runner string
	var access []string
	cmd := &cobra.Command{
		Use:   "create <integration>",
		Short: "Deploy a version of an integration",
		Long: `Deploy one version of an integration, and wait for it to be running.

Without --version it deploys the integration as it stands: its newest version
when nothing has changed since that was frozen, or otherwise the draft, frozen
first under the next tag (v1.2.0 becomes v1.2.1; an integration with no
versions starts at v0.1.0).

Every env var the integration declares needs a value: give it one with --env,
or point it at a cluster secret with --secret. ` + "`deployments options`" + ` lists
what a deploy of it needs.`,
		Example: `  octopaas deployments create billing
  octopaas deployments create billing --version v1.3.0
  octopaas deployments create billing --version v1.3.0 --replicas 2 \
    --env LOG_LEVEL=info --secret STRIPE_KEY=stripe --expose external --subdomain billing`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			env, err := f.bindings()
			if err != nil {
				return err
			}
			api, err := a.client()
			if err != nil {
				return err
			}

			var d client.Deployment
			var picked string
			err = a.ui.Spin(ui.Rocket, "Deploying "+args[0], func(l *ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				var versionID string
				if f.version != "" {
					if versionID, err = a.version(ctx, api, id, f.version); err != nil {
						return err
					}
				} else {
					//no version named: deploy what you see. The newest version when
					//the draft has not moved since, the draft frozen now otherwise.
					v, created, err := a.currentVersion(ctx, api, id)
					if err != nil {
						return err
					}
					versionID = v.ID
					picked = fmt.Sprintf("%s, the latest version: nothing changed since it was frozen", v.Tag)
					if created {
						picked = fmt.Sprintf("%s, the draft frozen just now", v.Tag)
					}
					l.Title("Deploying " + args[0] + " " + v.Tag)
				}
				settings := client.DeploymentSettings{
					SnapshotID: versionID, Replicas: replicaCount, Slug: slug, Expose: expose,
					Subdomain: subdomain, Env: env, Access: access, Runner: runner,
				}
				if cmd.Flags().Changed("tracing") {
					settings.Tracing = &f.tracing
				}
				d, err = api.CreateDeployment(ctx, id, settings)
				return err
			})
			if err != nil {
				return err
			}
			if picked != "" {
				a.ui.Success("Deploying %s", picked)
			}
			return a.settle(ctx, api, d, f, "Deployed")
		},
	}
	f.bind(cmd)
	cmd.Flags().IntVar(&replicaCount, "replicas", 0, "how many replicas to run (default: the platform's)")
	cmd.Flags().StringVar(&slug, "slug", "", "its internal address label (default: a free one)")
	cmd.Flags().StringVar(&expose, "expose", "", "external to publish it outside the cluster")
	cmd.Flags().StringVar(&subdomain, "subdomain", "", "the subdomain to publish it on, with --expose external")
	cmd.Flags().StringSliceVar(&access, "access", nil, "what the deployment's own token may do besides its stores: developer, operator")
	cmd.Flags().StringVar(&runner, "runner", "", "standard or agentic (default: picked from the integration)")
	return cmd
}

func (a *App) deploymentsOptionsCommand() *cobra.Command {

	var version, slug, expose string
	cmd := &cobra.Command{
		Use:   "options <integration>",
		Short: "Show what a deploy of an integration needs",
		Long:  `Show whether an integration serves HTTP, the env vars it declares, and whether a slug is free, before you deploy it.`,
		Example: `  octopaas deployments options billing --version v1.3.0
  octopaas deployments options billing --slug billing -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var opts map[string]any
			err = a.ui.Spin(ui.Tentacles, "Reading what "+args[0]+" needs", func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				versionID := ""
				if version != "" {
					if versionID, err = a.version(ctx, api, id, version); err != nil {
						return err
					}
				}
				opts, err = api.DeploymentOptions(ctx, id, versionID, slug, expose)
				return err
			})
			if err != nil {
				return err
			}
			//the options document is open-ended, so people get it as YAML,
			//which reads well enough without guessing at its shape.
			if a.output == outputTable {
				a.output = outputYAML
			}
			return a.emit(opts, nil)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "check against this version, by tag or id")
	cmd.Flags().StringVar(&slug, "slug", "", "check whether this slug is free")
	cmd.Flags().StringVar(&expose, "expose", "", "external to check external exposure")
	return cmd
}

func (a *App) deploymentsScaleCommand() *cobra.Command {

	var f deployFlags
	cmd := &cobra.Command{
		Use:     "scale <deployment> <replicas>",
		Short:   "Change how many replicas a deployment runs",
		Example: `  octopaas deployments scale billing 3`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			var n int
			if _, err := fmt.Sscan(args[1], &n); err != nil || n < 1 {
				return usageError{fmt.Errorf("replicas must be a whole number, at least 1, not %q", args[1])}
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			var d client.Deployment
			err = a.ui.Spin(ui.Rocket, fmt.Sprintf("Scaling %s to %d", args[0], n), func(*ui.Live) error {
				id, err := a.deployment(ctx, api, args[0])
				if err != nil {
					return err
				}
				d, err = api.ScaleDeployment(ctx, id, n)
				return err
			})
			if err != nil {
				return err
			}
			return a.settle(ctx, api, d, f, "Scaled")
		},
	}
	cmd.Flags().BoolVar(&f.noWait, "no-wait", false, "return once the platform accepts the change")
	cmd.Flags().DurationVar(&f.timeout, "timeout", 5*time.Minute, "how long to wait for it to be running")
	return cmd
}

func (a *App) deploymentsRolloutCommand() *cobra.Command {

	var f deployFlags
	cmd := &cobra.Command{
		Use:   "rollout <deployment>",
		Short: "Roll a deployment onto another version, env or tracing setting",
		Long: `Update a deployment in place, one replica at a time. Whatever you leave out
keeps what the deployment already has. A version is named by its tag in the
deployment's integration, or by id.`,
		Example: `  octopaas deployments rollout billing --version v1.4.0
  octopaas deployments rollout billing --env LOG_LEVEL=debug --tracing`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			env, err := f.bindings()
			if err != nil {
				return err
			}
			if f.version == "" && env == nil && !cmd.Flags().Changed("tracing") {
				return usageError{fmt.Errorf("say what to roll out: --version, --env, --secret or --tracing")}
			}
			api, err := a.client()
			if err != nil {
				return err
			}

			var d client.Deployment
			err = a.ui.Spin(ui.Rocket, "Rolling out "+args[0], func(*ui.Live) error {
				id, err := a.deployment(ctx, api, args[0])
				if err != nil {
					return err
				}
				rollout := client.Rollout{Env: env}
				current, err := api.GetDeployment(ctx, id)
				if err != nil {
					return err
				}
				for _, p := range current.Pods {
					f.replacing = append(f.replacing, p.Name)
				}
				if f.version != "" {
					if rollout.SnapshotID, err = a.version(ctx, api, current.IntegrationID, f.version); err != nil {
						return err
					}
				}
				if cmd.Flags().Changed("tracing") {
					rollout.Tracing = &f.tracing
				}
				d, err = api.RolloutDeployment(ctx, id, rollout)
				return err
			})
			if err != nil {
				return err
			}
			return a.settle(ctx, api, d, f, "Rolled out")
		},
	}
	f.bind(cmd)
	return cmd
}

func (a *App) deploymentsRemoveCommand() *cobra.Command {

	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <deployment>",
		Aliases: []string{"delete", "undeploy"},
		Short:   "Undeploy",
		Long:    `Take a deployment down. Its version stays, so you can deploy it again.`,
		Example: `  octopaas deployments rm billing --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			id, err := a.deployment(ctx, api, args[0])
			if err != nil {
				return err
			}
			if err := a.confirm(fmt.Sprintf("Take %s down?", args[0]), yes); err != nil {
				return err
			}
			err = a.ui.Spin(ui.Tentacles, "Undeploying "+args[0], func(*ui.Live) error {
				return api.DeleteDeployment(ctx, id)
			})
			if err == nil {
				a.ui.Moment(ui.Sleepy, "Undeployed "+args[0]+".", "Its versions are still there to deploy again.")
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask first")
	return cmd
}

// pollEvery is how often a wait checks on a deployment.
const pollEvery = 2 * time.Second

// settle waits for a deployment the platform just accepted to be running, then
// shows it. It is the part of deploying people actually watch.
func (a *App) settle(ctx context.Context, api *client.Client, d client.Deployment, f deployFlags, verb string) error {

	if f.noWait {
		return a.emit(d, func() { a.ui.Success("%s %s; the platform is on it (%s)", verb, a.ui.Bold(d.Name), d.Status) })
	}

	waitCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	err := a.ui.Spin(ui.Rocket, "Waiting for "+d.Name, func(l *ui.Live) error {

		//a fresh deployment can briefly read as failed before its first pod is
		//scheduled, so failure only counts once it has held for a few checks.
		failures := 0
		for {
			l.Detail(a.ui.Status(d.Status) + "  " + replicas(d))
			switch {
			case settled(d) && !runsAny(d, f.replacing):
				return nil
			case isFailed(d.Status):
				failures++
				if failures >= 3 {
					return fmt.Errorf("%s failed: %s", d.Name, firstOf(d.Reason, d.Status))
				}
			default:
				failures = 0
			}

			select {
			case <-waitCtx.Done():
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("%s is still %s after %s; check on it with `octopaas deployments get %s`", d.Name, d.Status, f.timeout, d.Name)
			case <-time.After(pollEvery):
			}
			next, err := api.GetDeployment(waitCtx, d.ID)
			if err != nil {
				return err
			}
			d = next
		}
	})
	if err != nil {
		return err
	}

	return a.emit(d, func() {
		lines := []string{
			fmt.Sprintf("%s  %s  %s", a.ui.Status(d.Status), d.Tag, replicas(d)),
		}
		if d.ExternalURL != "" {
			lines = append(lines, "", "→ "+d.ExternalURL)
		} else if d.InternalURL != "" {
			lines = append(lines, "", "→ "+d.InternalURL+" (inside the cluster)")
		}
		lines = append(lines, "", "Follow it with `octopaas logs "+d.Name+" -f`")
		a.ui.Box(fmt.Sprintf("%s %s", verb, d.Name), lines...)
	})
}

func settled(d client.Deployment) bool {

	if strings.ToLower(d.Status) != "running" {
		return false
	}
	want := d.DesiredReplicas
	if want == 0 {
		want = d.Replicas
	}
	return want == 0 || d.ReadyReplicas >= want
}

// runsAny says whether any of pods is still among d's.
func runsAny(d client.Deployment, pods []string) bool {

	for _, p := range d.Pods {
		if slices.Contains(pods, p.Name) {
			return true
		}
	}
	return false
}

func isFailed(status string) bool {

	switch strings.ToLower(status) {
	case "failed", "error", "crashloopbackoff":
		return true
	}
	return false
}

func replicas(d client.Deployment) string {

	want := d.DesiredReplicas
	if want == 0 {
		want = d.Replicas
	}
	return fmt.Sprintf("%d/%d ready", d.ReadyReplicas, want)
}
