package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

// testRunPoll is how often a test run being waited on is read again.
const testRunPoll = 2 * time.Second

// errTestsFailed is returned when a run did not pass, and errNotUpgraded when it
// passed but the rollout it asked for was not made, so the command exits 1.
var (
	errTestsFailed = errors.New("the tests did not pass")
	errNotUpgraded = errors.New("the tests passed, but the deployment was not upgraded")
)

func (a *App) deploymentsOutdatedCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "outdated",
		Short: "List deployments on an older runtime than this install deploys",
		Long: `List every deployment that runs an older runtime release than the one the
platform deploys now: the move a rollout would make, whether it is pinned,
and how its test suites last went on the new runtime. Deployments whose
version has no test suites are called out, since an upgrade cannot be
checked before it ships. With -o json or yaml only the outdated deployments
are listed.`,
		Example: `  octopaas deployments outdated
  octopaas deployments outdated -o json | jq -r '.deployments[] | select(.hasSuites | not) | .name'`,
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
			// Machine output is the outdated deployments alone, as the name
			// says; the table also calls out the untested ones that are not.
			outdated := view
			outdated.Deployments = nil
			for _, u := range view.Deployments {
				if u.UpgradeTo != "" {
					outdated.Deployments = append(outdated.Deployments, u)
				}
			}
			return a.emit(outdated, func() { a.printOutdated(view) })
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
	var untested, unknown []string
	for _, u := range view.Deployments {
		if !u.HasSuites {
			untested = append(untested, u.Name)
		}
		if u.RuntimeVersion == "" {
			unknown = append(unknown, u.Name)
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
	switch {
	case len(rows) == 0 && len(unknown) == 0:
		a.ui.Success("Every deployment runs %s.", view.RuntimeVersion)
	case len(rows) == 0:
		a.ui.Say("The platform deploys %s, and no deployment reports an older one.", a.ui.Bold(view.RuntimeVersion))
	default:
		a.ui.Say("The platform deploys %s.", a.ui.Bold(view.RuntimeVersion))
		a.ui.Table([]string{"NAME", "VERSION", "RUNTIME", "PIN", "TESTS", "ID"}, rows)
	}
	if len(unknown) > 0 {
		a.ui.Warn("No runtime reported yet, so these cannot be compared: %s", strings.Join(unknown, ", "))
	}
	if len(untested) > 0 {
		a.ui.Warn("No test suites, so an upgrade cannot be checked: %s", strings.Join(untested, ", "))
	}
}

// checkRuntime refuses a --runtime the platform does not support, listing the
// ones it does. Empty and latest need no asking.
func (a *App) checkRuntime(ctx context.Context, api *client.Client, runtime string) error {

	runtime = strings.TrimSpace(runtime)
	if runtime == "" || runtime == "latest" {
		return nil
	}
	rt, err := api.ListRuntimes(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(rt.Versions, runtime) {
		return nil
	}
	return usageError{fmt.Errorf("--runtime %s is not a supported release; pick one of %s, or latest",
		runtime, strings.Join(rt.Versions, ", "))}
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

The command waits for the verdict and exits 1 when the suites do not pass.
On an install that lends deployments an identity, rolling out needs a
signed-in session (octopaas auth login) rather than an API key.`,
		Example: `  octopaas deployments test billing
  octopaas deployments test billing --upgrade
  octopaas deployments test billing --version v1.5.0`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := testOnly
			if upgrade {
				mode = testRollout
			}
			return a.runTests(cmd.Context(), args[0], version, mode, noWait, timeout)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "the version to test, by tag or id (default: the one it runs)")
	cmd.Flags().BoolVar(&upgrade, "upgrade", false, "roll the deployment onto the tested version when every case passes")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "return once the run has started")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "how long to wait for the verdict")
	return cmd
}

// testMode is what a test run does once its suites pass.
type testMode int

const (
	// testOnly reports the verdict and changes nothing.
	testOnly testMode = iota
	// testRollout asks the platform to roll the deployment out on a pass, which
	// it refuses for a pinned deployment.
	testRollout
	// testUpgrade upgrades the deployment on a pass: through the platform for one
	// that follows the install, and by moving the pin for a pinned one.
	testUpgrade
)

// runTests starts a test run of ref and, unless noWait, follows it to its
// verdict and whatever mode does on a pass.
func (a *App) runTests(ctx context.Context, ref, version string, mode testMode, noWait bool, timeout time.Duration) error {

	api, err := a.client()
	if err != nil {
		return err
	}
	var run client.TestRun
	var current client.Deployment
	var movePin bool
	err = a.ui.Spin(ui.Tentacles, "Starting the tests of "+ref, func(*ui.Live) error {
		id, err := a.deployment(ctx, api, ref)
		if err != nil {
			return err
		}
		if current, err = api.GetDeployment(ctx, id); err != nil {
			return err
		}
		if mode == testUpgrade && current.UpgradeTo == "" {
			return fmt.Errorf("%s already runs the runtime this install deploys", ref)
		}
		// A pinned deployment is upgraded by moving its pin, which the platform
		// will not do on a pass: test it alone, and move the pin here after.
		movePin = mode == testUpgrade && current.RuntimePin != ""
		if movePin && noWait {
			return usageError{fmt.Errorf("%s is pinned, so its pin is moved after the tests pass: drop --no-wait", ref)}
		}
		snapshotID := ""
		if version != "" {
			if snapshotID, err = a.version(ctx, api, current.IntegrationID, version); err != nil {
				return err
			}
		}
		run, err = api.StartTestRun(ctx, id, snapshotID, mode != testOnly && !movePin)
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
		for !settledRun(run) {
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

	// What a pass does next, waited on until the replaced pods are gone.
	flags := deployFlags{timeout: timeout, replacing: podNames(current)}
	switch {
	case movePin:
		var d client.Deployment
		err := a.ui.Spin(ui.Rocket, "Moving "+ref+" to "+current.UpgradeTo, func(*ui.Live) error {
			d, err = api.RolloutDeployment(ctx, current.ID, upgradeRollout(current))
			return err
		})
		if err != nil {
			return err
		}
		return a.settle(ctx, api, d, flags, "Upgraded")
	case run.RolloutOnPass && run.RolledOutAt == nil:
		return errNotUpgraded
	case run.RolloutOnPass:
		d, err := api.GetDeployment(ctx, current.ID)
		if err != nil {
			return err
		}
		return a.settle(ctx, api, d, flags, "Upgraded")
	}
	return nil
}

// upgradeRollout is the rollout that moves d onto the install's runtime: no
// version, so the platform keeps the one it runs, and a pin moved to the new
// release so a pinned deployment stays pinned.
func upgradeRollout(d client.Deployment) client.Rollout {

	var ret client.Rollout
	if d.RuntimePin != "" {
		ret.RuntimeVersion = d.UpgradeTo
	}
	return ret
}

// podNames are a deployment's pods, which a rollout replaces.
func podNames(d client.Deployment) []string {

	ret := make([]string, 0, len(d.Pods))
	for _, p := range d.Pods {
		ret = append(ret, p.Name)
	}
	return ret
}

// settledRun reports whether a run has nothing more to say: it finished and,
// when it asked for a rollout on a pass, that rollout happened or failed.
func settledRun(run client.TestRun) bool {
	if !run.Finished() {
		return false
	}
	if run.Status == "passed" && run.RolloutOnPass {
		return run.RolledOutAt != nil || run.Error != ""
	}
	return true
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
every case passes; a pinned one is tested, then has its pin moved. On an
install that lends deployments an identity, upgrading needs a signed-in
session (octopaas auth login) rather than an API key.`,
		Example: `  octopaas deployments upgrade billing --test
  octopaas deployments upgrade billing`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			if test {
				return a.runTests(ctx, args[0], "", testUpgrade, noWait, timeout)
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			var d client.Deployment
			var current client.Deployment
			err = a.ui.Spin(ui.Rocket, "Upgrading "+args[0], func(*ui.Live) error {
				id, err := a.deployment(ctx, api, args[0])
				if err != nil {
					return err
				}
				if current, err = api.GetDeployment(ctx, id); err != nil {
					return err
				}
				if current.UpgradeTo == "" {
					return fmt.Errorf("%s already runs the runtime this install deploys", args[0])
				}
				d, err = api.RolloutDeployment(ctx, id, upgradeRollout(current))
				return err
			})
			if err != nil {
				return err
			}
			return a.settle(ctx, api, d, deployFlags{noWait: noWait, timeout: timeout, replacing: podNames(current)}, "Upgraded")
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
