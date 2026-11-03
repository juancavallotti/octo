package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

// testRunPoll is how often a test run being waited on is read again.
const testRunPoll = 2 * time.Second

// errTestsFailed is returned when a run did not pass, so the command exits 1.
var errTestsFailed = errors.New("the tests did not pass")

func (a *App) deploymentsOutdatedCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "outdated",
		Short: "List deployments on an older runtime than this install deploys",
		Long: `List every deployment that runs an older runtime release than the one the
platform deploys now: the move a rollout would make, whether it is pinned,
and how its test suites last went on the new runtime. Deployments whose
version has no test suites are called out, since an upgrade cannot be
checked before it ships.`,
		Example: `  octopaas deployments outdated
  octopaas deployments outdated -o json | jq '.deployments[] | select(.hasSuites | not)'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var view client.Upgrades
			err = a.ui.Spin(ui.Tentacles, "Comparing runtimes", func(*ui.Live) error {
				view, err = api.ListUpgrades(ctx)
				return err
			})
			if err != nil {
				return err
			}
			return a.emit(view, func() { a.printOutdated(view) })
		},
	}
	return cmd
}

func (a *App) printOutdated(view client.Upgrades) {

	if view.RuntimeVersion == "" {
		a.ui.Warn("The platform does not say which runtime it deploys, so nothing can be called outdated.")
		return
	}
	var rows [][]string
	var untested []string
	for _, u := range view.Deployments {
		if !u.HasSuites {
			untested = append(untested, u.Name)
		}
		if u.UpgradeTo == "" {
			continue
		}
		pinned := ""
		if u.Pinned {
			pinned = "pinned"
		}
		rows = append(rows, []string{u.Name, u.Tag, u.RuntimeVersion + " → " + u.UpgradeTo, pinned, a.testState(u, view.RuntimeVersion), u.DeploymentID})
	}
	if len(rows) == 0 {
		a.ui.Success("Every deployment runs %s.", view.RuntimeVersion)
	} else {
		a.ui.Say("The platform deploys %s.", a.ui.Bold(view.RuntimeVersion))
		a.ui.Table([]string{"NAME", "VERSION", "RUNTIME", "PIN", "TESTS", "ID"}, rows)
	}
	if len(untested) > 0 {
		a.ui.Warn("No test suites, so an upgrade cannot be checked: %s", strings.Join(untested, ", "))
	}
}

// testState says how a deployment's suites last went on the runtime it would
// move to.
func (a *App) testState(u client.Upgrade, runtime string) string {

	run := u.LatestRun
	switch {
	case !u.HasSuites:
		return a.ui.Dim("no tests")
	case run == nil || run.RuntimeVersion != runtime:
		return a.ui.Dim("not tested on " + runtime)
	case u.LatestRunStale && run.Finished():
		return a.ui.Dim("not tested since the last rollout")
	}
	return a.runVerdict(*run)
}

// runVerdict is one line on how a run went.
func (a *App) runVerdict(run client.TestRun) string {

	switch run.Status {
	case "passed":
		if run.RolledOutAt != nil {
			return a.ui.Status("passed") + " and upgraded"
		}
		return a.ui.Status("passed")
	case "failed":
		n := 0
		if run.Report != nil {
			n = run.Report.Totals.Failed + run.Report.Totals.Errored
		}
		return a.ui.Status("failed") + fmt.Sprintf(" (%d failing)", n)
	}
	return a.ui.Status(run.Status)
}

func (a *App) deploymentsTestCommand() *cobra.Command {

	var version string
	var upgrade, noWait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "test <deployment>",
		Short: "Run a deployment's test suites on the runtime this install deploys",
		Long: `Run the test suites frozen under a version of the deployment's integration —
by default the version it runs — with the deployment's env, on the runtime
release the platform deploys now. With --upgrade, a run in which every case
passes rolls the deployment onto that version; a pinned deployment refuses it.

The command waits for the verdict and exits 1 when the suites do not pass.`,
		Example: `  octopaas deployments test billing
  octopaas deployments test billing --upgrade
  octopaas deployments test billing --version v1.5.0`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTests(cmd.Context(), args[0], version, upgrade, noWait, timeout)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "the version to test, by tag or id (default: the one it runs)")
	cmd.Flags().BoolVar(&upgrade, "upgrade", false, "roll the deployment onto the tested version when every case passes")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "return once the run has started")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "how long to wait for the verdict")
	return cmd
}

// runTests starts a test run of ref and, unless noWait, follows it to its verdict.
func (a *App) runTests(ctx context.Context, ref, version string, upgrade, noWait bool, timeout time.Duration) error {

	api, err := a.client()
	if err != nil {
		return err
	}
	var run client.TestRun
	err = a.ui.Spin(ui.Tentacles, "Starting the tests of "+ref, func(*ui.Live) error {
		id, err := a.deployment(ctx, api, ref)
		if err != nil {
			return err
		}
		snapshotID := ""
		if version != "" {
			current, err := api.GetDeployment(ctx, id)
			if err != nil {
				return err
			}
			if snapshotID, err = a.version(ctx, api, current.IntegrationID, version); err != nil {
				return err
			}
		}
		run, err = api.StartTestRun(ctx, id, snapshotID, upgrade)
		return err
	})
	if err != nil {
		return err
	}
	if noWait {
		return a.emit(run, func() {
			a.ui.Success("The tests of %s are running on %s (run %s)", ref, run.RuntimeVersion, short(run.ID))
		})
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err = a.ui.Spin(ui.Tentacles, "Testing "+ref+" on "+run.RuntimeVersion, func(l *ui.Live) error {
		for !run.Finished() {
			select {
			case <-waitCtx.Done():
				return fmt.Errorf("the run is still going after %s; check it later with octopaas api /v1/deployments/%s/testruns/%s",
					timeout, run.DeploymentID, run.ID)
			case <-time.After(testRunPoll):
			}
			next, err := api.GetTestRun(waitCtx, run.DeploymentID, run.ID)
			if err != nil {
				return err
			}
			run = next
			l.Detail(a.ui.Status(run.Status))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := a.emit(run, func() { a.printRun(ref, run) }); err != nil {
		return err
	}
	if run.Status != "passed" {
		return errTestsFailed
	}
	return nil
}

func (a *App) printRun(ref string, run client.TestRun) {

	if run.Report != nil {
		t := run.Report.Totals
		a.ui.Say("%s on %s: %s  (%d passed, %d failed, %d errored, %d skipped)",
			ref, run.RuntimeVersion, a.runVerdict(run), t.Passed, t.Failed, t.Errored, t.Skipped)
		for _, s := range run.Report.Suites {
			for _, c := range s.Cases {
				if c.Status == "passed" || c.Status == "skipped" {
					continue
				}
				a.ui.Print("  %s %s: %s", a.ui.Status(c.Status), c.Name, firstOf(c.Summary, c.Error))
			}
		}
	} else {
		a.ui.Say("%s on %s: %s", ref, run.RuntimeVersion, a.runVerdict(run))
	}
	if run.Error != "" {
		a.ui.Warn("%s", run.Error)
	}
}

func (a *App) deploymentsUpgradeCommand() *cobra.Command {

	var test, noWait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "upgrade <deployment>",
		Short: "Move a deployment onto the runtime this install deploys",
		Long: `Roll a deployment onto the runtime release the platform deploys now, keeping its
version, env and tracing. A pinned deployment stays pinned, at the new release.

With --test the deployment's suites run first and it is upgraded only when
every case passes (the same as deployments test --upgrade).`,
		Example: `  octopaas deployments upgrade billing --test
  octopaas deployments upgrade billing`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			if test {
				return a.runTests(ctx, args[0], "", true, noWait, timeout)
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			var d client.Deployment
			err = a.ui.Spin(ui.Rocket, "Upgrading "+args[0], func(*ui.Live) error {
				id, err := a.deployment(ctx, api, args[0])
				if err != nil {
					return err
				}
				current, err := api.GetDeployment(ctx, id)
				if err != nil {
					return err
				}
				if current.UpgradeTo == "" {
					return fmt.Errorf("%s already runs the runtime this install deploys", args[0])
				}
				rollout := client.Rollout{SnapshotID: current.SnapshotID}
				if current.RuntimePin != "" {
					rollout.RuntimeVersion = current.UpgradeTo
				}
				d, err = api.RolloutDeployment(ctx, id, rollout)
				return err
			})
			if err != nil {
				return err
			}
			return a.settle(ctx, api, d, deployFlags{noWait: noWait, timeout: timeout}, "Upgraded")
		},
	}
	cmd.Flags().BoolVar(&test, "test", false, "run the deployment's test suites first, and upgrade only if they pass")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "return once the platform accepts the change")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "how long to wait")
	return cmd
}

func (a *App) deploymentsRuntimesCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "runtimes",
		Short: "List the runtime releases a deployment can be pinned to",
		Long: `List the runtime release the platform deploys and the supported releases no
newer than it. Pin a deployment with --runtime on deployments create or
rollout; --runtime latest follows the install again.`,
		Example: `  octopaas deployments runtimes
  octopaas deployments rollout billing --runtime 0.11.13`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var rt client.Runtimes
			err = a.ui.Spin(ui.Tentacles, "Reading runtimes", func(*ui.Live) error {
				rt, err = api.ListRuntimes(ctx)
				return err
			})
			if err != nil {
				return err
			}
			return a.emit(rt, func() {
				rows := make([][]string, 0, len(rt.Versions))
				for _, v := range rt.Versions {
					note := ""
					if v == rt.Current {
						note = "deployed by this install (latest)"
					}
					rows = append(rows, []string{v, note})
				}
				a.ui.Table([]string{"RUNTIME", ""}, rows)
			})
		},
	}
}
