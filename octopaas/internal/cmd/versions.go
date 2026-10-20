package cmd

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

func (a *App) versionsCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "versions",
		Aliases: []string{"version-tags", "ver"},
		Short:   "Freeze integrations into versions you can deploy",
		Long: `A version freezes an integration as it stands, definition and resources,
under a tag such as v1.3.0. Deployments run versions, never the live draft, so
creating one is how a change becomes deployable. The platform UI calls them
snapshots.

Versions are named by tag within their integration, or by id.`,
		Example: `  octopaas versions create billing v1.3.0
  octopaas versions list billing`,
	}
	cmd.AddCommand(a.versionsListCommand(), a.versionsCreateCommand(), a.versionsRemoveCommand(), a.versionsPullCommand())
	return cmd
}

func (a *App) versionsListCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "list <integration>",
		Aliases: []string{"ls"},
		Short:   "List an integration's versions, newest first",
		Example: `  octopaas versions list billing`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var all []client.Version
			err = a.ui.Spin(ui.Tentacles, "Listing versions of "+args[0], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				all, err = api.ListVersions(ctx, id)
				return err
			})
			if err != nil {
				return err
			}
			for i := range all {
				all[i].Definition = ""
			}
			return a.emit(all, func() {
				rows := make([][]string, 0, len(all))
				for _, v := range all {
					rows = append(rows, []string{v.Tag, v.ID, ago(v.CreatedAt)})
				}
				a.ui.Table([]string{"TAG", "ID", "CREATED"}, rows)
			})
		},
	}
}

func (a *App) versionsCreateCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "create <integration> <tag>",
		Short: "Freeze an integration under a tag",
		Example: `  octopaas versions create billing v1.3.0
  octopaas versions create billing "$(git describe --tags)"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var v client.Version
			err = a.ui.Spin(ui.Bubbles, "Freezing "+args[0]+" as "+args[1], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				v, err = api.CreateVersion(ctx, id, args[1])
				return err
			})
			if err != nil {
				return err
			}
			v.Definition = ""
			return a.emit(v, func() {
				a.ui.Success("Froze %s as %s (%s)", args[0], a.ui.Bold(v.Tag), v.ID)
				a.ui.Hint("Deploy it with `octopaas deployments create %s --version %s`.", args[0], v.Tag)
			})
		},
	}
}

func (a *App) versionsRemoveCommand() *cobra.Command {

	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <integration> <tag>",
		Aliases: []string{"delete"},
		Short:   "Delete a version nothing runs",
		Long:    `Delete a version. A version a deployment is running cannot be deleted; undeploy or roll that deployment onto another version first.`,
		Example: `  octopaas versions rm billing v1.0.0 --yes`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			versionID, err := a.versionOf(cmd, api, args[0], args[1])
			if err != nil {
				return err
			}
			if err := a.confirm(fmt.Sprintf("Delete version %s of %s?", args[1], args[0]), yes); err != nil {
				return err
			}
			err = a.ui.Spin(ui.Tentacles, "Deleting "+args[1], func(*ui.Live) error {
				return api.DeleteVersion(ctx, versionID)
			})
			if err == nil {
				a.ui.Success("Deleted version %s of %s", args[1], args[0])
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask first")
	return cmd
}

func (a *App) versionsPullCommand() *cobra.Command {

	var zipFile string
	var force bool
	cmd := &cobra.Command{
		Use:   "pull <integration> <tag> [folder]",
		Short: "Download one version into a folder",
		Long: `Download a version, definition and frozen resources, into a folder (by default
<integration>-<tag>), or with --zip as the bundle it came as.`,
		Example: `  octopaas versions pull billing v1.2.0
  octopaas versions pull billing v1.2.0 --zip billing-v1.2.0.zip`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			versionID, err := a.versionOf(cmd, api, args[0], args[1])
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			err = a.ui.Spin(ui.Ink, "Pulling "+args[0]+" "+args[1], func(l *ui.Live) error {
				_, err := api.DownloadVersionBundle(ctx, versionID, &buf, l.Transferred)
				return err
			})
			if err != nil {
				return err
			}
			folder := []string{args[0] + "-" + args[1]}
			if len(args) > 2 {
				folder = append(folder, args[2])
			}
			return a.save(buf.Bytes(), folder, zipFile, force)
		},
	}
	cmd.Flags().StringVar(&zipFile, "zip", "", "save the bundle as this .zip instead of unpacking it")
	cmd.Flags().BoolVar(&force, "force", false, "unpack into a folder that already has files")
	return cmd
}

// versionOf resolves an integration and one of its version tags to the
// version's id.
func (a *App) versionOf(cmd *cobra.Command, api *client.Client, integration, tag string) (string, error) {

	if isID(tag) {
		return tag, nil
	}
	id, err := a.integration(cmd.Context(), api, integration)
	if err != nil {
		return "", err
	}
	return a.version(cmd.Context(), api, id, tag)
}

// semverTag is a tag like v1.2.3, the one shape a next tag can be worked out
// from.
var semverTag = regexp.MustCompile(`^(v?)(\d+)\.(\d+)\.(\d+)$`)

// newest is the most recently created of versions, or false when there are none.
func newest(versions []client.Version) (client.Version, bool) {

	if len(versions) == 0 {
		return client.Version{}, false
	}
	ret := versions[0]
	for _, v := range versions[1:] {
		if v.CreatedAt.After(ret.CreatedAt) {
			ret = v
		}
	}
	return ret, true
}

// isCurrent says whether v is the integration as it stands: the same
// definition, and nothing edited after it was frozen.
func isCurrent(in client.Integration, v client.Version) bool {
	return v.Definition == in.Definition && !in.LastUpdated.After(v.CreatedAt)
}

// nextTag is the tag a freshly frozen draft gets: the highest semver tag with
// its patch bumped, v0.1.0 for an integration with no versions, and a dated
// draft tag when its tags follow no scheme we can extend.
func nextTag(versions []client.Version, now time.Time) string {

	taken := map[string]bool{}
	var best [3]int
	prefix, found := "v", false
	for _, v := range versions {
		taken[v.Tag] = true
		m := semverTag.FindStringSubmatch(v.Tag)
		if m == nil {
			continue
		}
		n := [3]int{atoi(m[2]), atoi(m[3]), atoi(m[4])}
		if !found || n[0] > best[0] || n[0] == best[0] && (n[1] > best[1] || n[1] == best[1] && n[2] > best[2]) {
			best, prefix, found = n, m[1], true
		}
	}

	switch {
	case found:
		for {
			best[2]++
			ret := fmt.Sprintf("%s%d.%d.%d", prefix, best[0], best[1], best[2])
			if !taken[ret] {
				return ret
			}
		}
	case len(versions) == 0:
		return "v0.1.0"
	default:
		return "draft-" + now.UTC().Format("20060102-150405")
	}
}

func atoi(s string) int {

	n, _ := strconv.Atoi(s)
	return n
}

// currentVersion is the version that is the integration as it stands: its
// newest one when nothing has changed since, or the draft frozen now under the
// next tag. created says which, so the command can tell you.
func (a *App) currentVersion(ctx context.Context, api *client.Client, integrationID string) (ret client.Version, created bool, err error) {

	in, err := api.GetIntegration(ctx, integrationID)
	if err != nil {
		return ret, false, err
	}
	versions, err := api.ListVersions(ctx, integrationID)
	if err != nil {
		return ret, false, err
	}
	if latest, ok := newest(versions); ok && isCurrent(in, latest) {
		return latest, false, nil
	}
	ret, err = api.CreateVersion(ctx, integrationID, nextTag(versions, time.Now()))
	return ret, err == nil, err
}
