package cmd

import (
	"context"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/ui"
	"github.com/juancavallotti/octo/octopaas/internal/update"
)

// EnvNoUpdateNotifier switches off the after-command notice of a new release.
const EnvNoUpdateNotifier = "OCTOPAAS_NO_UPDATE_NOTIFIER"

// noticeWait is the most a command waits, once a day, for the release check to
// come back before it exits; a check that is slower is picked up next time.
const noticeWait = 250 * time.Millisecond

func (a *App) releases() update.Source {
	return update.Default(a.userAgent())
}

func (a *App) upgradeCommand() *cobra.Command {

	var check bool
	var version string
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade octopaas to the newest release",
		Long: `Download the newest octopaas for this machine from the project's GitHub
releases, check it against the release's checksums.txt, and put it in place
of the binary you are running. A failure at any step leaves the binary you have
untouched.

octopaas also checks for a new release on its own, at most once a day, and
mentions it after a command finishes. It never checks in CI, without a
terminal, or with -o json, and OCTOPAAS_NO_UPDATE_NOTIFIER=1 turns it off.

A binary installed with ` + "`go install`" + ` is upgraded with go install, and one in
a folder you cannot write to needs sudo; octopaas says which.`,
		Example: `  octopaas upgrade
  octopaas upgrade --check
  octopaas upgrade --version v0.12.0`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.upgrade(cmd.Context(), check, version)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only say whether a newer release exists")
	cmd.Flags().StringVar(&version, "version", "", "install this release instead of the newest, such as v0.12.0")
	return cmd
}

type upgradeReport struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Newer   bool   `json:"newer"`
}

func (a *App) upgrade(ctx context.Context, check bool, version string) error {

	src := a.releases()
	current := "v" + a.Version

	target := version
	if target == "" {
		err := a.ui.Spin(ui.Tentacles, "Looking for the newest octopaas", func(*ui.Live) error {
			var err error
			target, err = src.Latest(ctx)
			return err
		})
		if err != nil {
			return err
		}
		//a deliberate check is as good as the daily one, so the notice can rest.
		_ = update.Record(a.home, update.State{CheckedAt: time.Now().UTC(), Latest: target})
	} else if !strings.HasPrefix(target, "v") {
		target = "v" + target
	}

	report := upgradeReport{Current: current, Latest: target, Newer: update.Newer(a.Version, target)}
	if check {
		return a.emit(report, func() {
			if report.Newer {
				a.ui.Moment(ui.Searching, fmt.Sprintf("octopaas %s is out; you have %s.", target, current), "Run `octopaas upgrade` to get it.")
				return
			}
			a.ui.Moment(ui.Happy, fmt.Sprintf("You have the newest octopaas (%s).", current), "")
		})
	}
	if version == "" && !report.Newer {
		a.ui.Moment(ui.Happy, fmt.Sprintf("You have the newest octopaas (%s).", current), "Nothing to do.")
		return nil
	}

	exe, err := update.Executable()
	if err != nil {
		return fmt.Errorf("find the running octopaas: %w", err)
	}
	if fromGoInstall(exe) {
		a.ui.Moment(ui.Confused, "This octopaas was installed with go install, so go install should upgrade it:",
			"go install github.com/juancavallotti/octo/octopaas@"+target)
		return nil
	}
	if !update.Writable(exe) {
		return usageError{fmt.Errorf("cannot write to %s; run `sudo octopaas upgrade`, or reinstall it somewhere you own", filepath.Dir(exe))}
	}

	err = a.ui.Spin(ui.Ink, fmt.Sprintf("Fetching octopaas %s", target), func(l *ui.Live) error {
		return src.Install(ctx, target, runtime.GOOS, runtime.GOARCH, exe, l.Transferred)
	})
	if err != nil {
		return err
	}

	a.ui.Banner(target)
	a.ui.Moment(ui.Happy, fmt.Sprintf("Upgraded octopaas %s → %s.", current, target),
		"What changed: https://github.com/juancavallotti/octo/releases/tag/"+target)
	return nil
}

// fromGoInstall says whether exe sits where go install puts binaries, which a
// self-replace would leave out of step with the module cache that built it.
func fromGoInstall(exe string) bool {

	dirs := []string{os.Getenv("GOBIN")}
	for _, p := range filepath.SplitList(build.Default.GOPATH) {
		dirs = append(dirs, filepath.Join(p, "bin"))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(d); err == nil {
			d = resolved
		}
		if filepath.Dir(exe) == filepath.Clean(d) {
			return true
		}
	}
	return false
}

// notifierOn is whether this command may mention a new release: only to a
// person at a terminal, and never from the commands about versions themselves.
func (a *App) notifierOn(cmd *cobra.Command) bool {

	switch cmd.Name() {
	case "upgrade", "version", "completion", "__complete", "__completeNoDesc", "help":
		return false
	}
	return a.ui.Fancy && a.Getenv(EnvNoUpdateNotifier) == "" && a.home != ""
}

// startUpdateCheck reads the cached answer and, once a day, asks for a fresh one
// in the background while the command runs.
func (a *App) startUpdateCheck(cmd *cobra.Command) {

	if !a.notifierOn(cmd) {
		return
	}
	state, fresh := update.Cached(a.home, time.Now())
	a.latest = state.Latest
	if fresh {
		return
	}
	a.refreshed = make(chan string, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if state, err := update.Refresh(ctx, a.releases(), a.home, time.Now()); err == nil {
			a.refreshed <- state.Latest
		}
	}()
}

// updateNotice mentions a newer release after the command's own output.
func (a *App) updateNotice() {

	latest := a.latest
	if a.refreshed != nil {
		select {
		case l := <-a.refreshed:
			latest = l
		case <-time.After(noticeWait):
		}
	}
	if latest == "" || !update.Newer(a.Version, latest) {
		return
	}
	a.ui.Say("")
	a.ui.Say("%s %s", a.ui.Accent("✦"), fmt.Sprintf("octopaas %s is out (you have v%s). Run `octopaas upgrade` to get it.", latest, a.Version))
}
